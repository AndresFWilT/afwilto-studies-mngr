package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

const foreignKeyViolation = "23503"

type SubjectRepository struct {
	pool *pgxpool.Pool
}

func NewSubjectRepository(pool *pgxpool.Pool) *SubjectRepository {
	return &SubjectRepository{pool: pool}
}

// subjectWriteError maps the alive-name unique index and the category guard
// (migration 0005) and the dependants guards (notes 0006, exams 0011) to domain errors.
func subjectWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.Code == uniqueViolation:
		return domain.NewConflict("name", domain.RuleDuplicateName, "a subject with this name already exists in the category")
	case pgErr.ConstraintName == "subject_in_use":
		return domain.NewConflict("id", domain.RuleInUse, "subject is still in use; move or delete what depends on it first")
	case pgErr.Code == foreignKeyViolation, pgErr.ConstraintName == "subject_category_not_found":
		return domain.SingleViolation("categoryId", "not_found", "category does not exist")
	}
	return err
}

func (r *SubjectRepository) Create(ctx context.Context, s domain.Subject) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO subjects (id, category_id, name, description, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		s.ID, s.CategoryID, s.Name.String(), s.Description, s.CreatedAt, s.UpdatedAt)
	return subjectWriteError(err)
}

func (r *SubjectRepository) Update(ctx context.Context, s domain.Subject) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE subjects
		SET category_id = $2, name = $3, description = $4, updated_at = $5, deleted_at = $6
		WHERE id = $1 AND deleted_at IS NULL`,
		s.ID, s.CategoryID, s.Name.String(), s.Description, s.UpdatedAt, s.DeletedAt)
	if err != nil {
		return subjectWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *SubjectRepository) Get(ctx context.Context, id string) (domain.Subject, error) {
	var (
		s    domain.Subject
		name string
	)
	err := r.pool.QueryRow(ctx, `
		SELECT id, category_id, name, description, created_at, updated_at
		FROM subjects WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&s.ID, &s.CategoryID, &name, &s.Description, &s.CreatedAt, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Subject{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Subject{}, err
	}
	s.Name = domain.RehydrateSubjectName(name)
	return s, nil
}

// aliveSubjects joins each alive subject to its alive category and the
// category's derived display path (the category repository's recursive tree).
const aliveSubjects = `
	SELECT s.id, s.category_id, s.name, s.description, s.created_at, s.updated_at,
	       t.id, t.name, t.color, t.path,
	       (SELECT count(*) FROM notes n WHERE n.subject_id = s.id AND n.deleted_at IS NULL)::int AS note_count
	FROM subjects s
	JOIN (` + aliveTree + `) t ON t.id = s.category_id
	WHERE s.deleted_at IS NULL`

func scanSubjectView(row pgx.Row) (domain.SubjectView, error) {
	var (
		v        domain.SubjectView
		name     string
		catColor string
		catName  string
		catPath  string
		catID    string
	)
	if err := row.Scan(&v.ID, &v.CategoryID, &name, &v.Description, &v.CreatedAt, &v.UpdatedAt,
		&catID, &catName, &catColor, &catPath, &v.NoteCount); err != nil {
		return domain.SubjectView{}, err
	}
	v.Name = domain.RehydrateSubjectName(name)
	v.Category = domain.CategoryRef{ID: catID, Name: catName, Color: catColor, Path: catPath}
	return v, nil
}

func (r *SubjectRepository) GetView(ctx context.Context, id string) (domain.SubjectView, error) {
	v, err := scanSubjectView(r.pool.QueryRow(ctx, aliveSubjects+` AND s.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.SubjectView{}, domain.ErrNotFound
	}
	return v, err
}

func (r *SubjectRepository) List(ctx context.Context, categoryID *string) ([]domain.SubjectView, error) {
	query := aliveSubjects
	args := []any{}
	if categoryID != nil {
		query += ` AND s.category_id = $1`
		args = append(args, *categoryID)
	}
	rows, err := r.pool.Query(ctx, query+` ORDER BY lower(s.name) COLLATE "C", s.id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	views := []domain.SubjectView{}
	for rows.Next() {
		v, err := scanSubjectView(rows)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, rows.Err()
}

func (r *SubjectRepository) CountAliveInCategory(ctx context.Context, categoryID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT count(*) FROM subjects WHERE category_id = $1 AND deleted_at IS NULL`, categoryID).Scan(&n)
	return n, err
}
