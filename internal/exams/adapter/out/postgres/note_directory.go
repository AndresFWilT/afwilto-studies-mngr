package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// NoteDirectory reads the studies `notes` table through SQL only: the two
// contexts share a database but never a Go import (Spec 18 D-10).
type NoteDirectory struct {
	pool *pgxpool.Pool
}

func NewNoteDirectory(pool *pgxpool.Pool) *NoteDirectory {
	return &NoteDirectory{pool: pool}
}

// FindNoteIDs joins the exam to its subject's alive notes and matches the
// titles exactly (case included). The exam need not be alive: the review of an
// old attempt keeps its links after the exam was deleted.
func (d *NoteDirectory) FindNoteIDs(ctx context.Context, examID string, titles []string) (map[string]string, error) {
	found := map[string]string{}
	if len(titles) == 0 {
		return found, nil
	}
	rows, err := d.pool.Query(ctx, `
		SELECT n.title, n.id
		FROM exams e JOIN notes n ON n.subject_id = e.subject_id
		WHERE e.id = $1 AND n.deleted_at IS NULL AND n.title = ANY($2)`, examID, titles)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var title, id string
		if err := rows.Scan(&title, &id); err != nil {
			return nil, err
		}
		found[title] = id
	}
	return found, rows.Err()
}
