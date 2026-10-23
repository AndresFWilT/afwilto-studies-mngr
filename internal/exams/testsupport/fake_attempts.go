package testsupport

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
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
	// Writes counts the Mutate calls that stored a change.
	Writes int
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

func cloneAttempt(a domain.Attempt) domain.Attempt {
	a.Items = slices.Clone(a.Items)
	return a
}

func (r *FakeAttemptRepo) Get(_ context.Context, id string) (domain.Attempt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.Rows[id]
	if !ok {
		return domain.Attempt{}, domain.ErrNotFound
	}
	return cloneAttempt(a), nil
}

// Mutate runs plan on a copy under the repository lock, like the SQL adapter's
// row lock, and stores the change it returns, even when it also returns an
// error.
func (r *FakeAttemptRepo) Mutate(_ context.Context, id string, plan func(domain.Attempt) (*out.AttemptChange, error)) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.Rows[id]
	if !ok {
		return domain.ErrNotFound
	}
	change, err := plan(cloneAttempt(a))
	if change != nil {
		a = cloneAttempt(a)
		if h := change.Header; h != nil {
			a.Status, a.SubmittedAt, a.ScorePercent, a.Passed = h.Status, h.SubmittedAt, h.ScorePercent, h.Passed
		}
		for _, it := range change.Items {
			a.Items[it.Position] = it
		}
		r.Rows[id] = a
		r.Writes++
	}
	return err
}

func (r *FakeAttemptRepo) ListByExam(_ context.Context, examID string) ([]domain.AttemptSummary, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := []domain.AttemptSummary{}
	for _, id := range r.Order {
		if a := r.Rows[id]; a.ExamID == examID {
			rows = append(rows, a.Summary())
		}
	}
	return rows, nil
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

// FakeNoteDirectory implements out.NoteDirectory over an in-memory list of
// alive notes per exam.
type FakeNoteDirectory struct {
	mu    sync.Mutex
	notes map[string]map[string]string // examID -> title -> note id
	// Err, when set, is what every lookup answers.
	Err error
}

func NewFakeNoteDirectory() *FakeNoteDirectory {
	return &FakeNoteDirectory{notes: map[string]map[string]string{}}
}

// Add makes a note alive in the subject of the exam.
func (d *FakeNoteDirectory) Add(examID, title, noteID string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.notes[examID] == nil {
		d.notes[examID] = map[string]string{}
	}
	d.notes[examID][title] = noteID
}

// Delete removes the note, as a logical delete would.
func (d *FakeNoteDirectory) Delete(examID, title string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.notes[examID], title)
}

func (d *FakeNoteDirectory) FindNoteIDs(_ context.Context, examID string, titles []string) (map[string]string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Err != nil {
		return nil, d.Err
	}
	found := map[string]string{}
	for _, title := range titles {
		if id, ok := d.notes[examID][title]; ok {
			found[title] = id
		}
	}
	return found, nil
}
