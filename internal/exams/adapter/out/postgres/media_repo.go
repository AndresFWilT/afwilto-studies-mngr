package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// MediaRepository reads the media table (migration 0012); the rows are
// written by ApplyImport, in the import's transaction.
type MediaRepository struct{ pool *pgxpool.Pool }

func NewMediaRepository(pool *pgxpool.Pool) *MediaRepository { return &MediaRepository{pool: pool} }

// Find returns the stored images among shas, by hash. Hashes that are not
// stored are absent from the map; an empty answer is not an error.
func (r *MediaRepository) Find(ctx context.Context, shas []string) (map[string]domain.Media, error) {
	out := map[string]domain.Media{}
	if len(shas) == 0 {
		return out, nil
	}
	rows, err := r.pool.Query(ctx,
		`SELECT sha256, content_type, bytes, width, height, created_at FROM media WHERE sha256 = ANY($1)`, shas)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m domain.Media
		if err := rows.Scan(&m.SHA256, &m.ContentType, &m.Bytes, &m.Width, &m.Height, &m.CreatedAt); err != nil {
			return nil, err
		}
		out[m.SHA256] = m
	}
	return out, rows.Err()
}
