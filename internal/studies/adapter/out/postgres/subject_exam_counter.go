package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SubjectExamCounter reads the exams table of the exams context through SQL
// only; the two contexts share a database but never a Go import (Spec 18 D-10).
type SubjectExamCounter struct {
	pool *pgxpool.Pool
}

func NewSubjectExamCounter(pool *pgxpool.Pool) *SubjectExamCounter {
	return &SubjectExamCounter{pool: pool}
}

func (c *SubjectExamCounter) CountAliveExams(ctx context.Context, subjectID string) (int, error) {
	var n int
	err := c.pool.QueryRow(ctx,
		`SELECT count(*)::int FROM exams WHERE subject_id = $1 AND deleted_at IS NULL`, subjectID).Scan(&n)
	return n, err
}
