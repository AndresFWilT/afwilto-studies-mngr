package postgres

import (
	"context"
	"errors"

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

func (d *NoteDirectory) FindNoteIDs(ctx context.Context, examID string, titles []string) (map[string]string, error) {
	return nil, errors.New("not implemented")
}
