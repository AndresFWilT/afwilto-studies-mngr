package out

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// AttemptRepository persists attempts and their frozen items (migration 0013).
type AttemptRepository interface {
	// Create stores the attempt and all its items in one transaction. When the
	// exam already has an in_progress attempt the answer is
	// domain.NewAttemptInProgress(existingID), enforced by the database;
	// domain.ErrNotFound when the exam is not alive.
	Create(ctx context.Context, attempt domain.Attempt) error

	// FindInProgress returns the id of the exam's open attempt; false when
	// there is none (a valid empty result, not an error).
	FindInProgress(ctx context.Context, examID string) (string, bool, error)
}

// SeedSource supplies the seed of a draw when the caller does not pick one.
type SeedSource interface {
	NewSeed() uint64
}
