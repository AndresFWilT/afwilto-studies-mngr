package testsupport

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// FixedClock answers one instant until a test moves it.
type FixedClock struct{ T time.Time }

func (c *FixedClock) Now() time.Time { return c.T }

// Advance moves the clock forward.
func (c *FixedClock) Advance(d time.Duration) { c.T = c.T.Add(d) }

// FixedSeeds is a SeedSource that yields Seed every time.
type FixedSeeds struct{ Seed uint64 }

func (s FixedSeeds) NewSeed() uint64 { return s.Seed }

// FakeAttemptRepo implements out.AttemptRepository in memory with the same
// rules as the SQL adapter: one in_progress attempt per exam, whole-attempt
// writes, alive exams only.
type FakeAttemptRepo struct {
	mu    sync.Mutex
	exams *FakeExamRepo

	Rows  map[string]domain.Attempt
	Order []string // creation order
	// HideOpen makes FindInProgress report none, so a test can reach the
	// conflict only Create sees (a start racing another start).
	HideOpen bool
}

func NewFakeAttemptRepo(exams *FakeExamRepo) *FakeAttemptRepo {
	return &FakeAttemptRepo{exams: exams, Rows: map[string]domain.Attempt{}}
}

func (r *FakeAttemptRepo) Create(ctx context.Context, a domain.Attempt) error {
	if _, err := r.exams.Get(ctx, a.ExamID); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, id := range r.Order {
		if other := r.Rows[id]; other.ExamID == a.ExamID && other.Status == domain.AttemptInProgress && a.Status == domain.AttemptInProgress {
			return domain.NewAttemptInProgress(other.ID)
		}
	}
	a.Items = slices.Clone(a.Items)
	r.Rows[a.ID] = a
	r.Order = append(r.Order, a.ID)
	return nil
}

func (r *FakeAttemptRepo) FindInProgress(_ context.Context, examID string) (string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.HideOpen {
		return "", false, nil
	}
	for _, id := range r.Order {
		if a := r.Rows[id]; a.ExamID == examID && a.Status == domain.AttemptInProgress {
			return a.ID, true, nil
		}
	}
	return "", false, nil
}

// Count is the number of stored attempts.
func (r *FakeAttemptRepo) Count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.Order)
}

// Close marks an attempt as no longer open (what 06b's submit does).
func (r *FakeAttemptRepo) Close(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.Rows[id]
	a.Status = domain.AttemptSubmitted
	r.Rows[id] = a
}
