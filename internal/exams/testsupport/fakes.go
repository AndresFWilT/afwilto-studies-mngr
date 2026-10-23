// Package testsupport provides hand-written fakes of the out-ports (never
// mocks of concrete classes) plus deterministic clock/id fixtures, shared by
// use-case and API tests. Fakes follow the same visibility and uniqueness
// rules as the SQL adapters. It never imports the studies context.
package testsupport

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
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

// FakeExamRepo implements out.ExamRepository in memory. Questions, when set
// (NewFakeQuestionBank does it), feeds the read model and the BR-E3 guard.
type FakeExamRepo struct {
	Rows      map[string]domain.Exam
	Subjects  *FakeSubjectDirectory
	Questions *FakeQuestionBank
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
	if r.Questions != nil && len(r.Questions.Bank(e.ID).Questions) > 0 {
		return domain.NewConflict("exam", domain.RuleInUse, "the exam still has questions")
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
	if err != nil {
		return domain.ExamSummary{}, err
	}
	summary := domain.ExamSummary{Exam: e}
	if r.Questions != nil {
		bank := r.Questions.Bank(id)
		summary.QuestionTotal = len(bank.Questions)
		for _, c := range bank.Classifications {
			for i := range c.Categories {
				for _, q := range bank.Questions {
					for _, ref := range q.Categories {
						if domain.Fold(ref.Classification) == domain.Fold(c.Name) && domain.Fold(ref.Category) == domain.Fold(c.Categories[i].Name) {
							c.Categories[i].QuestionCount++
						}
					}
				}
			}
			summary.Classifications = append(summary.Classifications, c)
		}
	}
	return summary, nil
}

func (r *FakeExamRepo) ListBySubject(_ context.Context, subjectID string) ([]domain.ExamSummary, error) {
	list := []domain.ExamSummary{}
	for _, e := range r.Rows {
		if e.SubjectID == subjectID && e.DeletedAt == nil {
			summary := domain.ExamSummary{Exam: e}
			if r.Questions != nil {
				summary.QuestionTotal = len(r.Questions.Bank(e.ID).Questions)
			}
			list = append(list, summary)
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

// FakeQuestionBank implements out.QuestionBank over per-exam in-memory banks.
// ApplyImport holds a lock, so it is as serial as the SQL adapter's exam lock.
type FakeQuestionBank struct {
	mu    sync.Mutex
	exams *FakeExamRepo
	banks map[string]domain.Bank
	// Applied counts the import writes that were persisted, Edits the exam
	// edits, merges and question deletes.
	Applied int
	Edits   int
}

// NewFakeQuestionBank wires the bank to the exam repository, which then reads
// question totals and classifications from it.
func NewFakeQuestionBank(exams *FakeExamRepo) *FakeQuestionBank {
	b := &FakeQuestionBank{exams: exams, banks: map[string]domain.Bank{}}
	exams.Questions = b
	return b
}

// Bank returns a copy of the stored bank of an exam.
func (b *FakeQuestionBank) Bank(examID string) domain.Bank {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.banks[examID].After(domain.ImportWrite{})
}

// Set replaces the stored bank, for tests that need a prepared state.
func (b *FakeQuestionBank) Set(examID string, bank domain.Bank) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.banks[examID] = bank
}

func (b *FakeQuestionBank) LoadBank(ctx context.Context, examID string) (domain.Bank, error) {
	if _, err := b.exams.Get(ctx, examID); err != nil {
		return domain.Bank{}, err
	}
	return b.Bank(examID), nil
}

func (b *FakeQuestionBank) ApplyImport(ctx context.Context, examID string, plan func(domain.Bank) (*domain.ImportWrite, error)) error {
	if _, err := b.exams.Get(ctx, examID); err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	current := b.banks[examID]
	write, err := plan(current.After(domain.ImportWrite{}))
	if err != nil || write == nil {
		return err
	}
	b.banks[examID] = current.After(*write)
	b.Applied++
	return nil
}

// ── slice 04: browser, single reads and edits ──────────────────────────────

// ListQuestions filters and sorts like the SQL adapter: origin set (ignoring
// case, then exactly), origin number, key (byte order), questions without a
// set or number last.
func (b *FakeQuestionBank) ListQuestions(ctx context.Context, examID string, f domain.QuestionFilter) ([]domain.QuestionSummary, error) {
	if _, err := b.exams.Get(ctx, examID); err != nil {
		return nil, err
	}
	bank := b.Bank(examID)

	var categoryName, categoryClass string
	for _, c := range bank.Classifications {
		for _, k := range c.Categories {
			if k.ID == f.CategoryID {
				categoryName, categoryClass = k.Name, c.Name
			}
		}
	}
	primary := ""
	for _, c := range bank.Classifications {
		if c.IsPrimary() {
			primary = c.Name
		}
	}
	has := func(q domain.Question, class, name string) bool {
		for _, ref := range q.Categories {
			if domain.Fold(ref.Classification) == domain.Fold(class) && domain.Fold(ref.Category) == domain.Fold(name) {
				return true
			}
		}
		return false
	}

	var rows []domain.Question
	for _, q := range bank.Questions {
		switch {
		case f.CategoryID != "" && !has(q, categoryClass, categoryName):
			continue
		case f.Origin != "" && (q.Origin == nil || q.Origin.Set != f.Origin):
			continue
		case f.Difficulty != "" && q.Difficulty != f.Difficulty:
			continue
		case f.Text != "" && !strings.Contains(strings.ToLower(q.Stem), strings.ToLower(f.Text)):
			continue
		}
		if f.Unclassified {
			classified := false
			for _, ref := range q.Categories {
				if primary != "" && domain.Fold(ref.Classification) == domain.Fold(primary) {
					classified = true
				}
			}
			if classified {
				continue
			}
		}
		rows = append(rows, q)
	}
	sort.SliceStable(rows, func(i, j int) bool {
		a, c := rows[i], rows[j]
		as, cs := originSet(a), originSet(c)
		if (as == nil) != (cs == nil) {
			return cs == nil
		}
		if as != nil && *as != *cs {
			if la, lc := strings.ToLower(*as), strings.ToLower(*cs); la != lc {
				return la < lc
			}
			return *as < *cs
		}
		an, cn := originNumber(a), originNumber(c)
		if (an == 0) != (cn == 0) {
			return cn == 0
		}
		if an != cn {
			return an < cn
		}
		return a.Key < c.Key
	})
	out := make([]domain.QuestionSummary, len(rows))
	for i, q := range rows {
		out[i] = domain.SummarizeQuestion(q)
	}
	return out, nil
}

func originSet(q domain.Question) *string {
	if q.Origin == nil || q.Origin.Set == "" {
		return nil
	}
	return &q.Origin.Set
}

func originNumber(q domain.Question) int {
	if q.Origin == nil {
		return 0
	}
	return q.Origin.Number
}

func (b *FakeQuestionBank) GetQuestion(ctx context.Context, id string) (domain.Question, error) {
	b.mu.Lock()
	var examID string
	for exam, bank := range b.banks {
		for _, q := range bank.Questions {
			if q.ID == id {
				examID = exam
			}
		}
	}
	b.mu.Unlock()
	if examID == "" {
		return domain.Question{}, domain.ErrNotFound
	}
	if _, err := b.exams.Get(ctx, examID); err != nil {
		return domain.Question{}, err
	}
	for _, q := range b.Bank(examID).Questions {
		if q.ID == id {
			return q, nil
		}
	}
	return domain.Question{}, domain.ErrNotFound
}

// ApplyEdit mirrors the SQL adapter: the plan sees the exam row and the bank,
// and the write lands as a whole or not at all.
func (b *FakeQuestionBank) ApplyEdit(ctx context.Context, examID string, plan func(domain.Exam, domain.Bank) (*domain.EditWrite, error)) error {
	exam, err := b.exams.Get(ctx, examID)
	if err != nil {
		return err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	current := b.banks[examID]
	write, err := plan(exam, current.After(domain.ImportWrite{}))
	if err != nil || write == nil {
		return err
	}
	if write.Exam != nil {
		if exam.Version != write.ExpectedVersion {
			return domain.NewConflict("version", domain.RuleStaleVersion, "the exam was changed since you read it")
		}
		if b.exams.nameClash(*write.Exam) {
			return duplicateName()
		}
		b.exams.Rows[examID] = *write.Exam
	}
	next := current.After(domain.ImportWrite{})
	applyEdit(&next, *write)
	b.banks[examID] = next
	b.Edits++
	return nil
}

// applyEdit changes the stored bank like the SQL statements do. Questions hold
// category references by name here, so they are resolved to ids before the
// structure changes and written back with the new names after it.
func applyEdit(bank *domain.Bank, w domain.EditWrite) {
	type pair struct{ class, cat string }
	byName := map[pair]string{}
	for _, c := range bank.Classifications {
		for _, k := range c.Categories {
			byName[pair{domain.Fold(c.Name), domain.Fold(k.Name)}] = k.ID
		}
	}
	linked := make([][]string, len(bank.Questions))
	for i, q := range bank.Questions {
		seen := map[string]bool{}
		for _, ref := range q.Categories {
			id := byName[pair{domain.Fold(ref.Classification), domain.Fold(ref.Category)}]
			if w.Merge != nil && id == w.Merge.From {
				id = w.Merge.Into
			}
			if id != "" && !seen[id] {
				seen[id] = true
				linked[i] = append(linked[i], id)
			}
		}
	}

	for _, changed := range w.Classifications {
		for i := range bank.Classifications {
			if bank.Classifications[i].ID == changed.ID {
				c := &bank.Classifications[i]
				c.Name, c.Fold, c.Position, c.WeightMode = changed.Name, changed.Fold, changed.Position, changed.WeightMode
			}
		}
	}
	for i := range bank.Classifications {
		c := &bank.Classifications[i]
		if w.Merge != nil && c.ID == w.Merge.ClassificationID {
			c.Categories = slices.DeleteFunc(c.Categories, func(k domain.Category) bool { return k.ID == w.Merge.From })
		}
		for j := range c.Categories {
			k := &c.Categories[j]
			for _, changed := range w.Categories {
				if changed.ID == k.ID {
					k.Name, k.Key, k.Fold, k.PaceQuestions, k.PaceMinutes = changed.Name, changed.Key, changed.Fold, changed.PaceQuestions, changed.PaceMinutes
				}
			}
			for _, cw := range w.Weights {
				if cw.CategoryID == k.ID {
					k.WeightPercent = cw.Percent
				}
			}
		}
	}

	names := map[string]domain.CategoryRef{}
	for _, c := range bank.Classifications {
		for _, k := range c.Categories {
			names[k.ID] = domain.CategoryRef{Classification: c.Name, Category: k.Name}
		}
	}
	var kept []domain.Question
	for i, q := range bank.Questions {
		if q.ID == w.DeleteQuestionID && w.DeleteQuestionID != "" {
			continue
		}
		q.Categories = nil
		for _, id := range linked[i] {
			q.Categories = append(q.Categories, names[id])
		}
		kept = append(kept, q)
	}
	bank.Questions = kept
}
