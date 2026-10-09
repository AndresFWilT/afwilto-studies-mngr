package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

type NoteKindRepository struct {
	pool *pgxpool.Pool
}

func NewNoteKindRepository(pool *pgxpool.Pool) *NoteKindRepository {
	return &NoteKindRepository{pool: pool}
}

// noteKindWriteError maps the alive-name unique index to a domain conflict.
func noteKindWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.NewConflict("name", domain.RuleDuplicateName, "a note kind with this name already exists")
	}
	return err
}

func (r *NoteKindRepository) Create(ctx context.Context, kind domain.NoteKind) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO note_kinds (id, name, created_at, updated_at)
		VALUES ($1, $2, $3, $4)`,
		kind.ID, kind.Name.String(), kind.CreatedAt, kind.UpdatedAt)
	return noteKindWriteError(err)
}

func (r *NoteKindRepository) Update(ctx context.Context, kind domain.NoteKind) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE note_kinds
		SET name = $2, updated_at = $3, deleted_at = $4
		WHERE id = $1 AND deleted_at IS NULL`,
		kind.ID, kind.Name.String(), kind.UpdatedAt, kind.DeletedAt)
	if err != nil {
		return noteKindWriteError(err)
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *NoteKindRepository) Get(ctx context.Context, id string) (domain.NoteKind, error) {
	var (
		kind domain.NoteKind
		name string
	)
	err := r.pool.QueryRow(ctx, `
		SELECT id, name, created_at, updated_at
		FROM note_kinds WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&kind.ID, &name, &kind.CreatedAt, &kind.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NoteKind{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.NoteKind{}, err
	}
	kind.Name = domain.RehydrateNoteKindName(name)
	return kind, nil
}

// aliveKinds selects alive kinds with their note count (0 until notes exist
// in Slice 05).
const aliveKinds = `
	SELECT id, name, created_at, updated_at, 0 AS note_count
	FROM note_kinds
	WHERE deleted_at IS NULL`

func scanKindView(row pgx.Row) (domain.NoteKindView, error) {
	var (
		v    domain.NoteKindView
		name string
	)
	if err := row.Scan(&v.ID, &name, &v.CreatedAt, &v.UpdatedAt, &v.NoteCount); err != nil {
		return domain.NoteKindView{}, err
	}
	v.Name = domain.RehydrateNoteKindName(name)
	return v, nil
}

func (r *NoteKindRepository) GetView(ctx context.Context, id string) (domain.NoteKindView, error) {
	v, err := scanKindView(r.pool.QueryRow(ctx, aliveKinds+` AND id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NoteKindView{}, domain.ErrNotFound
	}
	return v, err
}

func (r *NoteKindRepository) List(ctx context.Context) ([]domain.NoteKindView, error) {
	rows, err := r.pool.Query(ctx, aliveKinds+` ORDER BY lower(name) COLLATE "C", id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	views := []domain.NoteKindView{}
	for rows.Next() {
		v, err := scanKindView(rows)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, rows.Err()
}
