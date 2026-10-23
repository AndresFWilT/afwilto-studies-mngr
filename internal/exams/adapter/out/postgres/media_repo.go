package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// MediaRepository reads the media table (migration 0012); the rows are
// written by ApplyImport, in the import's transaction.
type MediaRepository struct{ pool *pgxpool.Pool }

func NewMediaRepository(pool *pgxpool.Pool) *MediaRepository { return &MediaRepository{pool: pool} }

func (r *MediaRepository) Find(ctx context.Context, shas []string) (map[string]domain.Media, error) {
	return nil, errors.New("not implemented")
}
