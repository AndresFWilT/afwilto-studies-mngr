package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

var errNotImplemented = errors.New("not implemented")

// AttemptRepository persists attempts and their frozen items (migration 0013).
type AttemptRepository struct{ pool *pgxpool.Pool }

func NewAttemptRepository(pool *pgxpool.Pool) *AttemptRepository {
	return &AttemptRepository{pool: pool}
}

func (r *AttemptRepository) Create(ctx context.Context, a domain.Attempt) error {
	return errNotImplemented
}

func (r *AttemptRepository) FindInProgress(ctx context.Context, examID string) (string, bool, error) {
	return "", false, errNotImplemented
}

// loadAttempt reads an attempt with its items back, for the round-trip tests
// and, from slice 06b on, the attempt reads.
func loadAttempt(ctx context.Context, q queryer, id string) (domain.Attempt, error) {
	return domain.Attempt{}, errNotImplemented
}

func encodeConfig(cfg domain.SessionConfig) ([]byte, error) { return nil, errNotImplemented }

func decodeConfig(raw []byte) (domain.SessionConfig, error) {
	return domain.SessionConfig{}, errNotImplemented
}

func encodeSnapshot(s domain.ItemSnapshot) ([]byte, error) { return nil, errNotImplemented }

func decodeSnapshot(raw []byte) (domain.ItemSnapshot, error) {
	return domain.ItemSnapshot{}, errNotImplemented
}
