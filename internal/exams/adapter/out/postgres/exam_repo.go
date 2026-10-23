package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
	checkViolation      = "23514"
)

type ExamRepository struct {
	pool *pgxpool.Pool
}

func NewExamRepository(pool *pgxpool.Pool) *ExamRepository {
	return &ExamRepository{pool: pool}
}

func duplicateName() error {
	return domain.NewConflict("name", domain.RuleDuplicateName, "an exam with this name already exists in the subject")
}

func subjectNotFound() error {
	return domain.SingleViolation("subjectId", "not_found", "subject does not exist")
}

// examWriteError maps the alive-name unique index and the subject foreign key
// (migration 0009) and the subject guard (migration 0011) to domain errors.
func examWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch pgErr.Code {
	case uniqueViolation:
		return duplicateName()
	case foreignKeyViolation:
		return subjectNotFound()
	case checkViolation:
		if pgErr.ConstraintName == "exam_subject_not_found" {
			return subjectNotFound()
		}
	}
	return err
}

// Create inserts only when the subject is alive: the foreign key alone would
// accept a logically deleted subject, so the insert selects from it.
func (r *ExamRepository) Create(ctx context.Context, e domain.Exam) error {
	tag, err := r.pool.Exec(ctx, `
		INSERT INTO exams (id, subject_id, name, question_count, time_limit_min, pass_percent, version, created_at, updated_at)
		SELECT $1, s.id, $3, $4, $5, $6, $7, $8, $9
		FROM subjects s WHERE s.id = $2 AND s.deleted_at IS NULL`,
		e.ID, e.SubjectID, e.Name.String(), e.QuestionCount, e.TimeLimitMin, e.PassPercent, e.Version, e.CreatedAt, e.UpdatedAt)
	if err != nil {
		return examWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return subjectNotFound()
	}
	return nil
}

// Update is the optimistic-concurrency write: it only matches the row at the
// version the caller read. Zero rows is then either a missing row or a stale
// version, told apart by a second look.
func (r *ExamRepository) Update(ctx context.Context, e domain.Exam, expectedVersion int) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE exams
		SET name = $3, question_count = $4, time_limit_min = $5, pass_percent = $6, version = $7, updated_at = $8
		WHERE id = $1 AND version = $2 AND deleted_at IS NULL`,
		e.ID, expectedVersion, e.Name.String(), e.QuestionCount, e.TimeLimitMin, e.PassPercent, e.Version, e.UpdatedAt)
	if err != nil {
		return examWriteError(err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}

	var alive bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM exams WHERE id = $1 AND deleted_at IS NULL)`, e.ID).Scan(&alive); err != nil {
		return err
	}
	if !alive {
		return domain.ErrNotFound
	}
	return domain.NewConflict("version", domain.RuleStaleVersion,
		"the exam was changed since you read it; reload and re-apply your edit")
}

// Delete stamps the exam deleted unless it still has alive questions (BR-E3).
// The exam row lock is the one imports take, so a delete and an import
// serialize and the question count is read after any import committed.
func (r *ExamRepository) Delete(ctx context.Context, e domain.Exam) (err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var id string
	if err = tx.QueryRow(ctx, `SELECT id FROM exams WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, e.ID).Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = domain.ErrNotFound
		}
		return err
	}
	var questions int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM questions WHERE exam_id = $1 AND deleted_at IS NULL`, e.ID).Scan(&questions); err != nil {
		return err
	}
	if questions > 0 {
		err = domain.NewConflict("exam", domain.RuleInUse,
			fmt.Sprintf("the exam still has %d questions; delete them first", questions))
		return err
	}
	if _, err = tx.Exec(ctx, `UPDATE exams SET deleted_at = $2 WHERE id = $1`, e.ID, e.DeletedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// aliveExams selects the alive exams.
const aliveExams = `
	SELECT id, subject_id, name, question_count, time_limit_min, pass_percent, version, created_at, updated_at
	FROM exams WHERE deleted_at IS NULL`

func scanExam(row pgx.Row) (domain.Exam, error) {
	var (
		e    domain.Exam
		name string
	)
	if err := row.Scan(&e.ID, &e.SubjectID, &name, &e.QuestionCount, &e.TimeLimitMin, &e.PassPercent,
		&e.Version, &e.CreatedAt, &e.UpdatedAt); err != nil {
		return domain.Exam{}, err
	}
	e.Name = domain.RehydrateExamName(name)
	return e, nil
}

func (r *ExamRepository) Get(ctx context.Context, id string) (domain.Exam, error) {
	e, err := scanExam(r.pool.QueryRow(ctx, aliveExams+` AND id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Exam{}, domain.ErrNotFound
	}
	return e, err
}

// GetSummary adds the alive question total and the classifications with their
// categories; LastSimulation arrives with slice 06b.
func (r *ExamRepository) GetSummary(ctx context.Context, id string) (domain.ExamSummary, error) {
	e, err := r.Get(ctx, id)
	if err != nil {
		return domain.ExamSummary{}, err
	}
	summary := domain.ExamSummary{Exam: e}
	if err := r.pool.QueryRow(ctx, `SELECT count(*) FROM questions WHERE exam_id = $1 AND deleted_at IS NULL`, id).Scan(&summary.QuestionTotal); err != nil {
		return domain.ExamSummary{}, err
	}
	if summary.Classifications, err = loadClassifications(ctx, r.pool, id); err != nil {
		return domain.ExamSummary{}, err
	}
	return summary, nil
}

func (r *ExamRepository) ListBySubject(ctx context.Context, subjectID string) ([]domain.ExamSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT e.id, e.subject_id, e.name, e.question_count, e.time_limit_min, e.pass_percent, e.version, e.created_at, e.updated_at,
		       (SELECT count(*) FROM questions q WHERE q.exam_id = e.id AND q.deleted_at IS NULL)
		FROM exams e WHERE e.deleted_at IS NULL AND e.subject_id = $1 ORDER BY lower(e.name) COLLATE "C", e.id`, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := []domain.ExamSummary{}
	for rows.Next() {
		var (
			e     domain.Exam
			name  string
			total int
		)
		if err := rows.Scan(&e.ID, &e.SubjectID, &name, &e.QuestionCount, &e.TimeLimitMin, &e.PassPercent, &e.Version, &e.CreatedAt, &e.UpdatedAt, &total); err != nil {
			return nil, err
		}
		e.Name = domain.RehydrateExamName(name)
		summaries = append(summaries, domain.ExamSummary{Exam: e, QuestionTotal: total})
	}
	return summaries, rows.Err()
}
