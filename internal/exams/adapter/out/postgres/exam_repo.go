package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

const (
	uniqueViolation     = "23505"
	foreignKeyViolation = "23503"
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
// (migration 0009) to domain errors.
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

func (r *ExamRepository) Delete(ctx context.Context, e domain.Exam) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE exams SET deleted_at = $2 WHERE id = $1 AND deleted_at IS NULL`, e.ID, e.DeletedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

// aliveExams selects the alive exams; the read model adds what later slices
// join in (question totals, the last simulation).
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

// GetSummary has QuestionTotal 0 and no LastSimulation until slices 03 and 06b.
func (r *ExamRepository) GetSummary(ctx context.Context, id string) (domain.ExamSummary, error) {
	e, err := r.Get(ctx, id)
	return domain.ExamSummary{Exam: e}, err
}

func (r *ExamRepository) ListBySubject(ctx context.Context, subjectID string) ([]domain.ExamSummary, error) {
	rows, err := r.pool.Query(ctx, aliveExams+` AND subject_id = $1 ORDER BY lower(name) COLLATE "C", id`, subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := []domain.ExamSummary{}
	for rows.Next() {
		e, err := scanExam(rows)
		if err != nil {
			return nil, err
		}
		summaries = append(summaries, domain.ExamSummary{Exam: e})
	}
	return summaries, rows.Err()
}
