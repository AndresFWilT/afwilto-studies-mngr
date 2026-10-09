package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

type TagQuery struct {
	pool *pgxpool.Pool
}

func NewTagQuery(pool *pgxpool.Pool) *TagQuery { return &TagQuery{pool: pool} }

// List counts alive links of alive notes; a tag with none is absent.
func (q *TagQuery) List(ctx context.Context) ([]domain.TagCount, error) {
	rows, err := q.pool.Query(ctx, `
		SELECT t.name, count(*)::int
		FROM tags t
		JOIN note_tags nt ON nt.tag_id = t.id AND nt.deleted_at IS NULL
		JOIN notes n ON n.id = nt.note_id AND n.deleted_at IS NULL
		GROUP BY t.name
		ORDER BY t.name COLLATE "C"`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := []domain.TagCount{}
	for rows.Next() {
		var c domain.TagCount
		if err := rows.Scan(&c.Name, &c.Count); err != nil {
			return nil, err
		}
		counts = append(counts, c)
	}
	return counts, rows.Err()
}
