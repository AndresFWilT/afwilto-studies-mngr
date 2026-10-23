package out

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// AttemptChange is what a Mutate plan wants stored: the attempt header (status,
// submittedAt, scorePercent, passed) when Header is set, and the answer state
// (selected, flagged, answeredAt, revealed, correct) of every item in Items.
type AttemptChange struct {
	Header *domain.Attempt
	Items  []domain.AttemptItem
}

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

	// Get returns the attempt with all its items, domain.ErrNotFound when it
	// does not exist. It takes no lock: use it for reads.
	Get(ctx context.Context, id string) (domain.Attempt, error)

	// Mutate loads the attempt under a row lock, hands it to plan, and stores the
	// change plan returns, all in one transaction. When plan returns a change and
	// an error, the change is stored and the error is returned: a request that
	// finds an attempt past its deadline persists the expiry and still answers
	// 409. When plan returns only an error, nothing is stored. domain.ErrNotFound
	// when the attempt does not exist.
	Mutate(ctx context.Context, id string, plan func(domain.Attempt) (*AttemptChange, error)) error

	// ListByExam returns the history row of every attempt of the exam, in no
	// particular order (domain.NewHistory orders them). An exam without
	// attempts is an empty result, not an error.
	ListByExam(ctx context.Context, examID string) ([]domain.AttemptSummary, error)
}

// SeedSource supplies the seed of a draw when the caller does not pick one.
type SeedSource interface {
	NewSeed() uint64
}
