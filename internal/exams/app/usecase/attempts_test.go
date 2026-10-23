package usecase

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
)

var t0 = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

type attemptEnv struct {
	env
	examID   string
	attempts *testsupport.FakeAttemptRepo
	clock    *testsupport.FixedClock
	preview  in.PreviewSession
	start    in.StartAttempt
}

func attemptQuestion(key string, refs ...domain.CategoryRef) domain.Question {
	q := domain.Question{ID: "q-" + key, Key: key, Type: domain.QuestionSingle, Stem: "stem " + key,
		Explanation: "explanation " + key, Version: 1, Categories: refs}
	for i, letter := range []string{"a", "b", "c", "d"} {
		q.Options = append(q.Options, domain.Option{Text: key + "/" + letter, Correct: i == 1, Rationale: "why " + key + "/" + letter})
	}
	return q
}

func manyQuestions(prefix string, n int, refs ...domain.CategoryRef) []domain.Question {
	out := make([]domain.Question, n)
	for i := range out {
		out[i] = attemptQuestion(fmt.Sprintf("%s-%02d", prefix, i), refs...)
	}
	return out
}

func dominio(name string) domain.CategoryRef {
	return domain.CategoryRef{Classification: "Dominio", Category: name}
}

// newAttemptEnv: an exam (65 questions / 130 min / 72%) whose bank has 40
// Seguridad and 40 Costos questions (Dominio, primary, 50/50) plus 5 with no
// Dominio.
func newAttemptEnv(t *testing.T) attemptEnv {
	t.Helper()
	e := newEnv()
	exam := e.add(t, subjectA, "SAA-C03")
	bank := domain.Bank{Classifications: []domain.Classification{{
		ID: "cl-dom", ExamID: exam.ID, Name: "Dominio", Fold: "dominio", Position: domain.PositionPrimary, WeightMode: domain.WeightManual,
		Categories: []domain.Category{
			{ID: "c-seg", ClassificationID: "cl-dom", Name: "Seguridad", Fold: "seguridad", WeightPercent: 50},
			{ID: "c-cos", ClassificationID: "cl-dom", Name: "Costos", Fold: "costos", WeightPercent: 50},
		},
	}}}
	bank.Questions = append(bank.Questions, manyQuestions("seg", 40, dominio("Seguridad"))...)
	bank.Questions = append(bank.Questions, manyQuestions("cos", 40, dominio("Costos"))...)
	bank.Questions = append(bank.Questions, manyQuestions("unc", 5)...)
	e.bank.Set(exam.ID, bank)

	clock := &testsupport.FixedClock{T: t0}
	attempts := testsupport.NewFakeAttemptRepo(e.repo)
	return attemptEnv{
		env: e, examID: exam.ID, attempts: attempts, clock: clock,
		preview: NewPreviewSessionService(e.repo, e.bank),
		start:   NewStartAttemptService(e.repo, e.bank, attempts, clock, &testsupport.SeqIDGen{N: 5000}, testsupport.FixedSeeds{Seed: 777}),
	}
}

func randomUntimed(count int) domain.SessionConfig {
	return domain.SessionConfig{
		Selection: domain.Selection{Kind: domain.SelectRandom, Count: count},
		Timing:    &domain.Timing{Kind: domain.Untimed}, Feedback: domain.FeedbackEnd,
	}
}

func (e attemptEnv) begin(t *testing.T, cfg domain.SessionConfig, seed *uint64) domain.AttemptView {
	t.Helper()
	view, err := e.start.Execute(ctx, e.examID, in.StartAttemptCommand{Config: cfg, Seed: seed})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	return view
}

func TestStartDrawsFreezesAndPersistsTheAttempt(t *testing.T) { // FR-9 snapshot
	e := newAttemptEnv(t)

	view := e.begin(t, randomUntimed(10), nil)

	stored, ok := e.attempts.Rows[view.ID]
	if !ok || e.attempts.Count() != 1 {
		t.Fatalf("stored attempts = %d, found %v", e.attempts.Count(), ok)
	}
	if stored.ExamID != e.examID || stored.Status != domain.AttemptInProgress || !stored.StartedAt.Equal(t0) || len(stored.Items) != 10 {
		t.Errorf("stored = %+v", stored)
	}
	correct := 0
	for _, it := range stored.Items {
		for _, o := range it.Snapshot.Options {
			if o.Correct {
				correct++
			}
			if o.Rationale == "" {
				t.Errorf("item %d lost a rationale: the snapshot must keep the answer key for 06b", it.Position)
			}
		}
	}
	if correct != 10 {
		t.Errorf("%d correct options across 10 items, want 10", correct)
	}
}

func TestStartOfATimedSessionSetsTheDeadlineFromTheClock(t *testing.T) { // AC-19 prerequisite: the server owns the deadline
	e := newAttemptEnv(t)
	cfg := randomUntimed(5)
	cfg.Timing = &domain.Timing{Kind: domain.Timed, Minutes: 30}

	view := e.begin(t, cfg, nil)

	if view.DeadlineAt == nil || !view.DeadlineAt.Equal(t0.Add(30*time.Minute)) {
		t.Errorf("deadline = %v, want clock + 30 min", view.DeadlineAt)
	}
	if view.RemainingSeconds == nil || *view.RemainingSeconds != 1800 {
		t.Errorf("remaining = %v, want 1800", view.RemainingSeconds)
	}
}

func TestStartOfAnUntimedSessionHasNoDeadlineOrRemainingTime(t *testing.T) {
	view := newAttemptEnv(t).begin(t, randomUntimed(5), nil)
	if view.DeadlineAt != nil || view.RemainingSeconds != nil {
		t.Errorf("deadline %v, remaining %v; want neither", view.DeadlineAt, view.RemainingSeconds)
	}
}

func TestStartOfASimulationIsTimedWithTheExamTimeAndFreezesTheMarks(t *testing.T) { // AC-14, FR-7
	e := newAttemptEnv(t)

	view := e.begin(t, domain.SessionConfig{Selection: domain.Selection{Kind: domain.SelectSimulation}}, nil)

	if len(view.Items) != 65 || view.SuggestedMinutes != 130 || view.PassPercent != 72 {
		t.Errorf("items %d, suggested %d, pass %d; want 65, 130, 72", len(view.Items), view.SuggestedMinutes, view.PassPercent)
	}
	if view.DeadlineAt == nil || !view.DeadlineAt.Equal(t0.Add(130*time.Minute)) {
		t.Errorf("deadline = %v, want clock + 130 min", view.DeadlineAt)
	}
	counts := map[string]int{}
	for _, it := range view.Items {
		counts[it.Snapshot.Categories[0].Category]++
	}
	if counts["Seguridad"] != 33 || counts["Costos"] != 32 {
		t.Errorf("per domain = %v, want 33/32 (65 over 50/50, the tie to the first)", counts)
	}
}

func TestStartWithoutASeedUsesTheSeedSourceAndStoresIt(t *testing.T) { // slice 06a §1.3 Seed
	e := newAttemptEnv(t)

	view := e.begin(t, randomUntimed(5), nil)

	if view.Seed != 777 {
		t.Errorf("seed = %d, want the source's 777", view.Seed)
	}
}

func TestStartKeepsAnExplicitSeedEvenZero(t *testing.T) {
	for _, seed := range []uint64{0, 1, 18446744073709551615} {
		e := newAttemptEnv(t)
		if got := e.begin(t, randomUntimed(5), &seed).Seed; got != seed {
			t.Errorf("seed = %d, want %d", got, seed)
		}
	}
}

func snapshotsOf(v domain.AttemptView) string {
	var parts []string
	for _, it := range v.Items {
		var labels []string
		for _, o := range it.Snapshot.Options {
			labels = append(labels, o.Label+"="+o.Text)
		}
		parts = append(parts, it.Snapshot.Key+"["+strings.Join(labels, " ")+"]")
	}
	return strings.Join(parts, ";")
}

func TestTheSameSeedOnAnUnchangedBankGivesTheSameAttempt(t *testing.T) { // AC-17
	seed := uint64(2024)
	a := newAttemptEnv(t).begin(t, randomUntimed(20), &seed)
	b := newAttemptEnv(t).begin(t, randomUntimed(20), &seed)

	if snapshotsOf(a) == "" || snapshotsOf(a) != snapshotsOf(b) {
		t.Errorf("same seed, different attempts:\n%s\n%s", snapshotsOf(a), snapshotsOf(b))
	}
	other := seed + 1
	if snapshotsOf(newAttemptEnv(t).begin(t, randomUntimed(20), &other)) == snapshotsOf(a) {
		t.Error("another seed drew the same attempt")
	}
}

func TestStartingWithTimingOnASimulationIsRejectedAndStoresNothing(t *testing.T) { // S06a-5
	e := newAttemptEnv(t)
	cfg := domain.SessionConfig{Selection: domain.Selection{Kind: domain.SelectSimulation}, Timing: &domain.Timing{Kind: domain.Timed, Minutes: 30}}

	_, err := e.start.Execute(ctx, e.examID, in.StartAttemptCommand{Config: cfg})

	wantViolation(t, err, "config.timing", domain.RuleLockedBySimulation)
	if e.attempts.Count() != 0 {
		t.Error("a rejected start stored an attempt")
	}
}

func TestASecondStartWhileOneIsInProgressConflictsAndCarriesTheExistingId(t *testing.T) { // S06a-6, BR-A1
	e := newAttemptEnv(t)
	first := e.begin(t, randomUntimed(5), nil)

	_, err := e.start.Execute(ctx, e.examID, in.StartAttemptCommand{Config: randomUntimed(5)})

	wantConflict(t, err, domain.RuleAttemptInProgress)
	c := err.(*domain.ConflictError)
	if c.Field != "attemptId" || !strings.Contains(c.Message, first.ID) {
		t.Errorf("conflict = %+v, want field attemptId and the id %s in the message", c, first.ID)
	}
	if e.attempts.Count() != 1 {
		t.Errorf("%d attempts stored, want only the first", e.attempts.Count())
	}
}

func TestAnInProgressAttemptOfAnotherExamDoesNotBlock(t *testing.T) { // BR-A1 is per exam
	e := newAttemptEnv(t)
	e.begin(t, randomUntimed(5), nil)
	other := e.add(t, subjectA, "AIF-C01")
	e.bank.Set(other.ID, domain.Bank{Questions: manyQuestions("x", 3)})

	if _, err := e.start.Execute(ctx, other.ID, in.StartAttemptCommand{Config: randomUntimed(2)}); err != nil {
		t.Errorf("start on another exam: %v", err)
	}
}

func TestAFinishedAttemptDoesNotBlockANewStart(t *testing.T) { // BR-A1 counts in_progress only
	e := newAttemptEnv(t)
	first := e.begin(t, randomUntimed(5), nil)
	e.attempts.Close(first.ID)

	if _, err := e.start.Execute(ctx, e.examID, in.StartAttemptCommand{Config: randomUntimed(5)}); err != nil {
		t.Errorf("start after the first closed: %v", err)
	}
}

func TestAConflictOnlyTheRepositorySeesStillSurfacesAsAttemptInProgress(t *testing.T) { // two starts racing
	e := newAttemptEnv(t)
	first := e.begin(t, randomUntimed(5), nil)
	e.attempts.HideOpen = true

	_, err := e.start.Execute(ctx, e.examID, in.StartAttemptCommand{Config: randomUntimed(5)})

	wantConflict(t, err, domain.RuleAttemptInProgress)
	if !strings.Contains(err.Error(), first.ID) {
		t.Errorf("err = %v, want the existing id %s", err, first.ID)
	}
}

func TestStartWithAShortCategoryFailsWithItsNumbersAndStoresNothing(t *testing.T) { // AC-15
	e := newAttemptEnv(t)
	bank := e.bank.Bank(e.examID)
	bank.Questions = slices.DeleteFunc(bank.Questions, func(q domain.Question) bool {
		return strings.HasPrefix(q.Key, "seg-") && q.Key > "seg-11" // 12 Seguridad left
	})
	e.bank.Set(e.examID, bank)

	_, err := e.start.Execute(ctx, e.examID, in.StartAttemptCommand{Config: domain.SessionConfig{Selection: domain.Selection{Kind: domain.SelectSimulation}}})

	var v *domain.ValidationError
	if !asValidation(err, &v) || len(v.Violations) != 1 || v.Violations[0].Rule != domain.RuleInsufficientQuestions ||
		v.Violations[0].Shortage == nil || *v.Violations[0].Shortage != (domain.Shortage{Category: "Seguridad", Needed: 33, Available: 12}) {
		t.Errorf("err = %v, want insufficient_questions {Seguridad, needed 33, available 12}", err)
	}
	if e.attempts.Count() != 0 {
		t.Error("a failed start stored an attempt")
	}
}

func TestStartOnAnUnknownOrDeletedExamIsNotFound(t *testing.T) {
	e := newAttemptEnv(t)
	gone := e.add(t, subjectA, "Gone")
	if err := e.delete.Execute(ctx, gone.ID); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{gone.ID, "0abcdef0-0000-7000-8000-ffffffffffff"} {
		if _, err := e.start.Execute(ctx, id, in.StartAttemptCommand{Config: randomUntimed(1)}); !isNotFound(err) {
			t.Errorf("start on %s: %v, want ErrNotFound", id, err)
		}
		if _, err := e.preview.Execute(ctx, id, randomUntimed(1)); !isNotFound(err) {
			t.Errorf("preview on %s: %v, want ErrNotFound", id, err)
		}
	}
}

func TestPreviewIsAPureReadAndIgnoresAnOpenAttempt(t *testing.T) { // slice 06a §1.2
	e := newAttemptEnv(t)
	e.begin(t, randomUntimed(5), nil)
	before := e.attempts.Count()

	p, err := e.preview.Execute(ctx, e.examID, domain.SessionConfig{Selection: domain.Selection{Kind: domain.SelectSimulation}})

	if err != nil {
		t.Fatalf("preview with an attempt open: %v", err)
	}
	if p.PoolSize != 80 || p.DrawCount != 65 || p.SuggestedMinutes != 130 || len(p.Shortages) != 0 {
		t.Errorf("preview = %+v, want pool 80, draw 65, 130 min", p)
	}
	if e.attempts.Count() != before {
		t.Error("a preview stored an attempt")
	}
}

func TestPreviewRejectsABrokenConfig(t *testing.T) {
	e := newAttemptEnv(t)
	cfg := domain.SessionConfig{Selection: domain.Selection{Kind: domain.SelectSimulation}, Feedback: domain.FeedbackEach}

	_, err := e.preview.Execute(ctx, e.examID, cfg)

	wantViolation(t, err, "config.feedback", domain.RuleLockedBySimulation)
}

func TestPreviewAndStartAgreeOnTheShortages(t *testing.T) { // slice 06a §1.2 "the same check the start runs"
	e := newAttemptEnv(t)
	bank := e.bank.Bank(e.examID)
	bank.Questions = slices.DeleteFunc(bank.Questions, func(q domain.Question) bool { return strings.HasPrefix(q.Key, "cos-") && q.Key > "cos-04" })
	e.bank.Set(e.examID, bank)
	cfg := domain.SessionConfig{Selection: domain.Selection{Kind: domain.SelectSimulation}}

	p, _ := e.preview.Execute(ctx, e.examID, cfg)
	_, err := e.start.Execute(ctx, e.examID, in.StartAttemptCommand{Config: cfg})

	var v *domain.ValidationError
	if !asValidation(err, &v) || len(p.Shortages) != 1 || *v.Violations[0].Shortage != p.Shortages[0] {
		t.Errorf("preview shortages %v, start error %v; want the same single shortage", p.Shortages, err)
	}
}

func asValidation(err error, target **domain.ValidationError) bool { return errors.As(err, target) }
