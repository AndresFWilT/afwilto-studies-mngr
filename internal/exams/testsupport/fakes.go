// Package testsupport provides hand-written fakes of the out-ports (never
// mocks of concrete classes) plus deterministic clock/id fixtures, shared by
// use-case and API tests. Fakes follow the same visibility and uniqueness
// rules as the SQL adapters. It never imports the studies context.
package testsupport

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

var FixedNow = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

// StepClock starts at FixedNow and advances one minute per call, so ordering
// by timestamps is observable in tests.
type StepClock struct{ N int }

func (c *StepClock) Now() time.Time {
	c.N++
	return FixedNow.Add(time.Duration(c.N) * time.Minute)
}

// SeqIDGen yields valid, ordered UUIDs that contain hex letters, so tests
// exercise case handling the way real UUIDs would.
type SeqIDGen struct{ N int }

func (g *SeqIDGen) NewID() string {
	g.N++
	return fmt.Sprintf("0abcdef0-0000-7000-8000-%012d", g.N)
}

// FakeSubjectDirectory implements out.SubjectDirectory over a set of alive
// subject ids; Err, when set, is returned by every call.
type FakeSubjectDirectory struct {
	Alive map[string]bool
	Err   error
}

func NewFakeSubjectDirectory(aliveIDs ...string) *FakeSubjectDirectory {
	d := &FakeSubjectDirectory{Alive: map[string]bool{}}
	for _, id := range aliveIDs {
		d.Alive[id] = true
	}
	return d
}

func (d *FakeSubjectDirectory) IsAlive(_ context.Context, subjectID string) (bool, error) {
	return d.Alive[subjectID], d.Err
}

// FakeExamRepo implements out.ExamRepository in memory.
type FakeExamRepo struct {
	Rows     map[string]domain.Exam
	Subjects *FakeSubjectDirectory
}

func NewFakeExamRepo(subjects *FakeSubjectDirectory) *FakeExamRepo {
	return &FakeExamRepo{Rows: map[string]domain.Exam{}, Subjects: subjects}
}

func (r *FakeExamRepo) nameClash(e domain.Exam) bool {
	for id, other := range r.Rows {
		if id != e.ID && other.DeletedAt == nil && other.SubjectID == e.SubjectID && other.Name.Key() == e.Name.Key() {
			return true
		}
	}
	return false
}

func duplicateName() error {
	return domain.NewConflict("name", domain.RuleDuplicateName, "an exam with this name already exists in the subject")
}

func (r *FakeExamRepo) Create(_ context.Context, e domain.Exam) error {
	if !r.Subjects.Alive[e.SubjectID] {
		return domain.SingleViolation("subjectId", "not_found", "subject does not exist")
	}
	if r.nameClash(e) {
		return duplicateName()
	}
	r.Rows[e.ID] = e
	return nil
}

func (r *FakeExamRepo) Update(_ context.Context, e domain.Exam, expectedVersion int) error {
	current, ok := r.Rows[e.ID]
	if !ok || current.DeletedAt != nil {
		return domain.ErrNotFound
	}
	if current.Version != expectedVersion {
		return domain.NewConflict("version", domain.RuleStaleVersion, "the exam was changed since you read it")
	}
	if r.nameClash(e) {
		return duplicateName()
	}
	r.Rows[e.ID] = e
	return nil
}

func (r *FakeExamRepo) Delete(_ context.Context, e domain.Exam) error {
	current, ok := r.Rows[e.ID]
	if !ok || current.DeletedAt != nil {
		return domain.ErrNotFound
	}
	current.DeletedAt = e.DeletedAt
	r.Rows[e.ID] = current
	return nil
}

func (r *FakeExamRepo) Get(_ context.Context, id string) (domain.Exam, error) {
	e, ok := r.Rows[id]
	if !ok || e.DeletedAt != nil {
		return domain.Exam{}, domain.ErrNotFound
	}
	return e, nil
}

func (r *FakeExamRepo) GetSummary(ctx context.Context, id string) (domain.ExamSummary, error) {
	e, err := r.Get(ctx, id)
	return domain.ExamSummary{Exam: e}, err
}

func (r *FakeExamRepo) ListBySubject(_ context.Context, subjectID string) ([]domain.ExamSummary, error) {
	list := []domain.ExamSummary{}
	for _, e := range r.Rows {
		if e.SubjectID == subjectID && e.DeletedAt == nil {
			list = append(list, domain.ExamSummary{Exam: e})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Name.Key() != list[j].Name.Key() {
			return list[i].Name.Key() < list[j].Name.Key()
		}
		return list[i].ID < list[j].ID
	})
	return list, nil
}
