package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

type NoteRepository struct {
	pool *pgxpool.Pool
}

func NewNoteRepository(pool *pgxpool.Pool) *NoteRepository {
	return &NoteRepository{pool: pool}
}

// noteWriteError maps the alive-title unique index and the reference guard
// (migration 0006) to domain errors.
func noteWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	switch {
	case pgErr.Code == uniqueViolation:
		return domain.NewConflict("title", domain.RuleDuplicateName, "a note with this title already exists in the subject")
	case pgErr.ConstraintName == "note_subject_not_found":
		return domain.SingleViolation("subjectId", "not_found", "subject does not exist")
	case pgErr.ConstraintName == "note_kind_not_found":
		return domain.SingleViolation("kindId", "not_found", "note kind does not exist")
	}
	return err
}

func (r *NoteRepository) Create(ctx context.Context, n domain.Note) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO notes (id, subject_id, kind_id, title, content, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		n.ID, n.SubjectID, n.KindID, n.Title.String(), n.Content.String(), n.Version, n.CreatedAt, n.UpdatedAt)
	return noteWriteError(err)
}

// Update is the optimistic-concurrency write: it only matches the row at the
// version the caller read. Zero rows is then either a missing row or a stale
// version, told apart by a second look.
func (r *NoteRepository) Update(ctx context.Context, n domain.Note, expectedVersion int) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE notes
		SET subject_id = $3, kind_id = $4, title = $5, content = $6, version = $7, updated_at = $8
		WHERE id = $1 AND version = $2 AND deleted_at IS NULL`,
		n.ID, expectedVersion, n.SubjectID, n.KindID, n.Title.String(), n.Content.String(), n.Version, n.UpdatedAt)
	if err != nil {
		return noteWriteError(err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var alive bool
	if err := r.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM notes WHERE id = $1 AND deleted_at IS NULL)`, n.ID).Scan(&alive); err != nil {
		return err
	}
	if !alive {
		return domain.ErrNotFound
	}
	return domain.NewConflict("version", domain.RuleStaleVersion,
		"the note was changed since you read it; reload and re-apply your edit")
}

func (r *NoteRepository) Delete(ctx context.Context, n domain.Note) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE notes SET deleted_at = $2 WHERE id = $1 AND deleted_at IS NULL`, n.ID, n.DeletedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (r *NoteRepository) Get(ctx context.Context, id string) (domain.Note, error) {
	var (
		n              domain.Note
		title, content string
	)
	err := r.pool.QueryRow(ctx, `
		SELECT id, subject_id, kind_id, title, content, version, created_at, updated_at
		FROM notes WHERE id = $1 AND deleted_at IS NULL`, id).
		Scan(&n.ID, &n.SubjectID, &n.KindID, &title, &content, &n.Version, &n.CreatedAt, &n.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Note{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Note{}, err
	}
	n.Title = domain.RehydrateNoteTitle(title)
	n.Content = domain.RehydrateMarkdownContent(content)
	return n, nil
}

// aliveNotes selects an alive note with its kind. contentExpr is the content
// column for a full read and an empty literal for list summaries, so lists
// never ship the Markdown body.
func aliveNotes(contentExpr string) string {
	return `
	SELECT n.id, n.subject_id, n.kind_id, n.title, ` + contentExpr + `, n.version, n.created_at, n.updated_at,
	       k.id, k.name
	FROM notes n
	JOIN note_kinds k ON k.id = n.kind_id
	WHERE n.deleted_at IS NULL`
}

func scanNoteView(row pgx.Row) (domain.NoteView, error) {
	var (
		v                        domain.NoteView
		title, content, kindName string
	)
	if err := row.Scan(&v.ID, &v.SubjectID, &v.KindID, &title, &content, &v.Version, &v.CreatedAt, &v.UpdatedAt,
		&v.Kind.ID, &kindName); err != nil {
		return domain.NoteView{}, err
	}
	v.Title = domain.RehydrateNoteTitle(title)
	v.Content = domain.RehydrateMarkdownContent(content)
	v.Kind.Name = kindName
	v.Tags = []string{} // tags arrive in Slice 06
	return v, nil
}

func (r *NoteRepository) GetView(ctx context.Context, id string) (domain.NoteView, error) {
	v, err := scanNoteView(r.pool.QueryRow(ctx, aliveNotes("n.content")+` AND n.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.NoteView{}, domain.ErrNotFound
	}
	return v, err
}

// orderBy builds the ORDER BY from the closed SortSpec vocabulary; nothing
// user-supplied reaches the SQL text. Ties break by title, then id, ascending.
func orderBy(sort domain.SortSpec) string {
	column := `lower(n.title) COLLATE "C"`
	switch sort.Field {
	case domain.SortByCreatedAt:
		column = `n.created_at`
	case domain.SortByUpdatedAt:
		column = `n.updated_at`
	}
	direction := " ASC"
	if sort.Desc {
		direction = " DESC"
	}
	return ` ORDER BY ` + column + direction + `, lower(n.title) COLLATE "C" ASC, n.id ASC`
}

func (r *NoteRepository) ListBySubject(ctx context.Context, subjectID string, sort domain.SortSpec) ([]domain.NoteView, error) {
	rows, err := r.pool.Query(ctx, aliveNotes("''")+` AND n.subject_id = $1`+orderBy(sort), subjectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	views := []domain.NoteView{}
	for rows.Next() {
		v, err := scanNoteView(rows)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, rows.Err()
}

func (r *NoteRepository) CountAliveBySubject(ctx context.Context, subjectID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM notes WHERE subject_id = $1 AND deleted_at IS NULL`, subjectID).Scan(&n)
	return n, err
}

func (r *NoteRepository) CountAliveByKind(ctx context.Context, kindID string) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx,
		`SELECT count(*) FROM notes WHERE kind_id = $1 AND deleted_at IS NULL`, kindID).Scan(&n)
	return n, err
}
