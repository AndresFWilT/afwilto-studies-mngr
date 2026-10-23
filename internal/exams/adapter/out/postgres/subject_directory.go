package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SubjectDirectory reads the studies `subjects` table through SQL only: the
// two contexts share a database but never a Go import (Spec 18 D-10).
type SubjectDirectory struct {
	pool *pgxpool.Pool
}

func NewSubjectDirectory(pool *pgxpool.Pool) *SubjectDirectory {
	return &SubjectDirectory{pool: pool}
}

func (d *SubjectDirectory) IsAlive(ctx context.Context, subjectID string) (bool, error) {
	var alive bool
	err := d.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM subjects WHERE id = $1 AND deleted_at IS NULL)`, subjectID).Scan(&alive)
	return alive, err
}
