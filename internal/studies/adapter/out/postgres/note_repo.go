package postgres

import (
	"context"
	"encoding/json"
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

// slabLinksJSON is the jsonb shape of notes.slab_links.
type slabLinkJSON struct {
	Label string `json:"label"`
	Path  string `json:"path"`
}

func encodeSlabLinks(links []domain.SlabLink) ([]byte, error) {
	out := make([]slabLinkJSON, len(links))
	for i, l := range links {
		out[i] = slabLinkJSON{Label: l.Label, Path: l.Path}
	}
	return json.Marshal(out)
}

func decodeSlabLinks(raw []byte) ([]domain.SlabLink, error) {
	var in []slabLinkJSON
	if err := json.Unmarshal(raw, &in); err != nil {
		return nil, err
	}
	out := make([]domain.SlabLink, len(in))
	for i, l := range in {
		out[i] = domain.SlabLink{Label: l.Label, Path: l.Path}
	}
	return out, nil
}

// syncTags makes the note's alive links equal its tag set, inside the note's
// transaction. Links that left the set get deleted_at; a returning tag clears
// deleted_at on the same row. Tag names are inserted in sorted order, so
// concurrent writers lock the unique index in the same order.
func syncTags(ctx context.Context, tx pgx.Tx, n domain.Note) error {
	names := n.Tags.Names()
	if _, err := tx.Exec(ctx, `
		UPDATE note_tags nt SET deleted_at = $3
		FROM tags t
		WHERE nt.tag_id = t.id AND nt.note_id = $1 AND nt.deleted_at IS NULL AND NOT (t.name = ANY($2))`,
		n.ID, names, n.UpdatedAt); err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO tags (name) SELECT unnest($1::text[]) ON CONFLICT (name) DO NOTHING`, names); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO note_tags (note_id, tag_id, created_at)
		SELECT $1, t.id, $3 FROM tags t WHERE t.name = ANY($2)
		ON CONFLICT (note_id, tag_id) DO UPDATE SET deleted_at = NULL
		WHERE note_tags.deleted_at IS NOT NULL`,
		n.ID, names, n.UpdatedAt)
	return err
}

func (r *NoteRepository) Create(ctx context.Context, n domain.Note) error {
	links, err := encodeSlabLinks(n.SlabLinks)
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `
		INSERT INTO notes (id, subject_id, kind_id, title, content, slab_links, version, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8, $9)`,
		n.ID, n.SubjectID, n.KindID, n.Title.String(), n.Content.String(), links, n.Version, n.CreatedAt, n.UpdatedAt); err != nil {
		return noteWriteError(err)
	}
	if err := syncTags(ctx, tx, n); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Update is the optimistic-concurrency write: it only matches the row at the
// version the caller read. Zero rows is then either a missing row or a stale
// version, told apart by a second look.
func (r *NoteRepository) Update(ctx context.Context, n domain.Note, expectedVersion int) error {
	links, err := encodeSlabLinks(n.SlabLinks)
	if err != nil {
		return err
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	tag, err := tx.Exec(ctx, `
		UPDATE notes
		SET subject_id = $3, kind_id = $4, title = $5, content = $6, slab_links = $7::jsonb,
		    version = $8, updated_at = $9
		WHERE id = $1 AND version = $2 AND deleted_at IS NULL`,
		n.ID, expectedVersion, n.SubjectID, n.KindID, n.Title.String(), n.Content.String(), links, n.Version, n.UpdatedAt)
	if err != nil {
		return noteWriteError(err)
	}
	if tag.RowsAffected() == 1 {
		if err := syncTags(ctx, tx, n); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	_ = tx.Rollback(ctx)

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

// aliveTags aggregates a note's alive tag names, sorted in byte order.
const aliveTags = `COALESCE((
		SELECT array_agg(t.name ORDER BY t.name COLLATE "C")
		FROM note_tags nt JOIN tags t ON t.id = nt.tag_id
		WHERE nt.note_id = n.id AND nt.deleted_at IS NULL), '{}')::text[]`

// noteColumns selects an alive note with its kind and tags. For list summaries
// content is an empty literal and slab_links an empty array, so lists never
// ship the Markdown body or the links.
func noteColumns(full bool) string {
	content, links := `''`, `'[]'::jsonb`
	if full {
		content, links = `n.content`, `n.slab_links`
	}
	return `
	SELECT n.id, n.subject_id, n.kind_id, n.title, ` + content + `, ` + links + `, n.version, n.created_at, n.updated_at,
	       k.id, k.name, ` + aliveTags
}

func aliveNotes(full bool) string {
	return noteColumns(full) + `
	FROM notes n
	JOIN note_kinds k ON k.id = n.kind_id
	WHERE n.deleted_at IS NULL`
}

func (r *NoteRepository) Get(ctx context.Context, id string) (domain.Note, error) {
	v, err := r.GetView(ctx, id)
	if err != nil {
		return domain.Note{}, err
	}
	return v.Note, nil
}

// noteScan holds one row of noteColumns.
type noteScan struct {
	v        domain.NoteView
	title    string
	content  string
	links    []byte
	kindName string
	tags     []string
}

func (s *noteScan) dest() []any {
	return []any{&s.v.ID, &s.v.SubjectID, &s.v.KindID, &s.title, &s.content, &s.links,
		&s.v.Version, &s.v.CreatedAt, &s.v.UpdatedAt, &s.v.Kind.ID, &s.kindName, &s.tags}
}

func (s *noteScan) view() (domain.NoteView, error) {
	links, err := decodeSlabLinks(s.links)
	if err != nil {
		return domain.NoteView{}, err
	}
	v := s.v
	v.Title = domain.RehydrateNoteTitle(s.title)
	v.Content = domain.RehydrateMarkdownContent(s.content)
	v.Kind.Name = s.kindName
	v.Tags = domain.RehydrateTagSet(s.tags)
	v.SlabLinks = links
	return v, nil
}

func scanNoteView(row pgx.Row) (domain.NoteView, error) {
	var s noteScan
	if err := row.Scan(s.dest()...); err != nil {
		return domain.NoteView{}, err
	}
	return s.view()
}

func (r *NoteRepository) GetView(ctx context.Context, id string) (domain.NoteView, error) {
	v, err := scanNoteView(r.pool.QueryRow(ctx, aliveNotes(true)+` AND n.id = $1`, id))
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

const hasAliveTag = ` AND EXISTS (
		SELECT 1 FROM note_tags nt JOIN tags t ON t.id = nt.tag_id
		WHERE nt.note_id = n.id AND nt.deleted_at IS NULL AND t.name = `

func (r *NoteRepository) ListBySubject(ctx context.Context, subjectID string, tag *string, sort domain.SortSpec) ([]domain.NoteView, error) {
	query := aliveNotes(false) + ` AND n.subject_id = $1`
	args := []any{subjectID}
	if tag != nil {
		query += hasAliveTag + `$2)`
		args = append(args, *tag)
	}
	rows, err := r.pool.Query(ctx, query+orderBy(sort), args...)
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

// ListByTag adds each note's subject and category (with the derived path).
func (r *NoteRepository) ListByTag(ctx context.Context, tag string, sort domain.SortSpec) ([]domain.NoteView, error) {
	rows, err := r.pool.Query(ctx, noteColumns(false)+`,
	       s.id, s.name, c.id, c.name, c.color, c.path
	FROM notes n
	JOIN note_kinds k ON k.id = n.kind_id
	JOIN subjects s ON s.id = n.subject_id
	JOIN (`+aliveTree+`) c ON c.id = s.category_id
	WHERE n.deleted_at IS NULL`+hasAliveTag+`$1)`+orderBy(sort), tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	views := []domain.NoteView{}
	for rows.Next() {
		var (
			ns  noteScan
			sub domain.SubjectRef
			cat domain.CategoryRef
		)
		if err := rows.Scan(append(ns.dest(), &sub.ID, &sub.Name, &cat.ID, &cat.Name, &cat.Color, &cat.Path)...); err != nil {
			return nil, err
		}
		v, err := ns.view()
		if err != nil {
			return nil, err
		}
		v.Subject, v.Category = &sub, &cat
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
