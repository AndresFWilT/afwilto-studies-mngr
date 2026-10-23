package postgres

import (
	"errors"
	"reflect"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// ── Get ─────────────────────────────────────────────────────────────────────

func TestAttemptRepoGetReturnsTheAttemptWithItsItems(t *testing.T) { // slice 06b §1.2
	e := newAttemptEnv(t)
	want := e.start(t, 3)

	got, err := e.attempts.Get(ctx, want.ID)

	if err != nil || !reflect.DeepEqual(got, normalizeAttempt(want)) {
		t.Errorf("Get = %+v (err %v), want %+v", got, err, normalizeAttempt(want))
	}
}

func TestAttemptRepoGetOfAnUnknownAttemptIsNotFound(t *testing.T) {
	e := newAttemptEnv(t)
	if _, err := e.attempts.Get(ctx, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// ── Mutate ──────────────────────────────────────────────────────────────────

func answerChange(a domain.Attempt, position int, labels []string, flagged, revealed bool, correct *bool, at time.Time) *out.AttemptChange {
	it := a.Items[position]
	it.Selected, it.Flagged, it.Revealed, it.Correct, it.AnsweredAt = labels, flagged, revealed, correct, &at
	return &out.AttemptChange{Items: []domain.AttemptItem{it}}
}

func TestAttemptRepoMutateStoresTheAnswerStateOfTheChangedItemsOnly(t *testing.T) {
	e := newAttemptEnv(t)
	a := e.start(t, 3)
	yes := true
	at := time.Date(2026, 10, 23, 9, 30, 0, 0, time.UTC)

	err := e.attempts.Mutate(ctx, a.ID, func(cur domain.Attempt) (*out.AttemptChange, error) {
		if len(cur.Items) != 3 {
			t.Errorf("plan saw %d items, want 3", len(cur.Items))
		}
		return answerChange(cur, 1, []string{"B", "D"}, true, true, &yes, at), nil
	})

	if err != nil {
		t.Fatal(err)
	}
	got, _ := e.attempts.Get(ctx, a.ID)
	it := got.Items[1]
	if !slices.Equal(it.Selected, []string{"B", "D"}) || !it.Flagged || !it.Revealed || it.Correct == nil || !*it.Correct || it.AnsweredAt == nil || !it.AnsweredAt.Equal(at) {
		t.Errorf("item 1 = %+v", it)
	}
	if !reflect.DeepEqual(got.Items[0], normalizeAttempt(a).Items[0]) || !reflect.DeepEqual(got.Items[2], normalizeAttempt(a).Items[2]) {
		t.Error("an item outside the change was rewritten")
	}
	if got.Status != domain.AttemptInProgress || got.ScorePercent != nil {
		t.Errorf("header changed without a Header in the change: %q %v", got.Status, got.ScorePercent)
	}
}

func TestAttemptRepoMutateStoresTheClosedHeaderAndTheScoredItems(t *testing.T) {
	e := newAttemptEnv(t)
	a := e.start(t, 3)
	at := a.StartedAt.Add(9 * time.Minute)

	err := e.attempts.Mutate(ctx, a.ID, func(cur domain.Attempt) (*out.AttemptChange, error) {
		cur.Items[0].Selected = cur.Items[0].Snapshot.CorrectLabels()
		closed := cur.Submit(at)
		return &out.AttemptChange{Header: &closed, Items: closed.Items}, nil
	})

	if err != nil {
		t.Fatal(err)
	}
	got, _ := e.attempts.Get(ctx, a.ID)
	if got.Status != domain.AttemptSubmitted || got.SubmittedAt == nil || !got.SubmittedAt.Equal(at) || got.ScorePercent == nil || *got.ScorePercent != 33.33 || got.Passed != nil {
		t.Errorf("header = %q, submittedAt %v, score %v, passed %v; want submitted, 33.33", got.Status, got.SubmittedAt, got.ScorePercent, got.Passed)
	}
	for _, it := range got.Items {
		if it.Correct == nil {
			t.Errorf("item %d has no stored correctness", it.Position)
		}
	}
	if got.Items[0].Correct == nil || !*got.Items[0].Correct {
		t.Error("item 0 was answered with its key and must be stored correct")
	}
	if _, found, _ := e.attempts.FindInProgress(ctx, e.exam.ID); found {
		t.Error("the closed attempt still counts as open")
	}
}

func TestAttemptRepoMutateKeepsATwoDecimalScoreExactly(t *testing.T) { // numeric(5,2) round trip
	e := newAttemptEnv(t)
	a := e.start(t, 1)
	score := 72.31
	err := e.attempts.Mutate(ctx, a.ID, func(cur domain.Attempt) (*out.AttemptChange, error) {
		closed := cur.Submit(cur.StartedAt.Add(time.Minute))
		closed.ScorePercent = &score
		return &out.AttemptChange{Header: &closed}, nil
	})
	got, _ := e.attempts.Get(ctx, a.ID)
	if err != nil || got.ScorePercent == nil || *got.ScorePercent != 72.31 {
		t.Errorf("score = %v (err %v), want 72.31", got.ScorePercent, err)
	}
}

func TestAttemptRepoMutateStoresTheChangeAndStillReturnsThePlansError(t *testing.T) { // the expiry a refused write persists
	e := newAttemptEnv(t)
	a := e.start(t, 2)
	refusal := domain.NewAttemptExpired()

	err := e.attempts.Mutate(ctx, a.ID, func(cur domain.Attempt) (*out.AttemptChange, error) {
		expired := cur.Expire()
		return &out.AttemptChange{Header: &expired, Items: expired.Items}, refusal
	})

	if !errors.Is(err, refusal) {
		t.Errorf("err = %v, want the plan's error", err)
	}
	got, _ := e.attempts.Get(ctx, a.ID)
	if got.Status != domain.AttemptExpired || got.ScorePercent == nil {
		t.Errorf("status %q, score %v; want the expiry stored although the plan failed", got.Status, got.ScorePercent)
	}
}

func TestAttemptRepoMutateStoresNothingWhenThePlanOnlyReturnsAnError(t *testing.T) {
	e := newAttemptEnv(t)
	a := e.start(t, 2)
	boom := errors.New("refused")

	err := e.attempts.Mutate(ctx, a.ID, func(cur domain.Attempt) (*out.AttemptChange, error) { return nil, boom })

	got, _ := e.attempts.Get(ctx, a.ID)
	if !errors.Is(err, boom) || !reflect.DeepEqual(got, normalizeAttempt(a)) {
		t.Errorf("err = %v, attempt changed = %v; want the error and an untouched attempt", err, !reflect.DeepEqual(got, normalizeAttempt(a)))
	}
}

func TestAttemptRepoMutateWithoutAChangeIsAPlainRead(t *testing.T) {
	e := newAttemptEnv(t)
	a := e.start(t, 2)
	var seen domain.Attempt
	if err := e.attempts.Mutate(ctx, a.ID, func(cur domain.Attempt) (*out.AttemptChange, error) { seen = cur; return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(seen, normalizeAttempt(a)) {
		t.Errorf("plan saw %+v", seen)
	}
}

func TestAttemptRepoMutateOfAnUnknownAttemptIsNotFoundAndNeverCallsThePlan(t *testing.T) {
	e := newAttemptEnv(t)
	err := e.attempts.Mutate(ctx, uuid.NewString(), func(domain.Attempt) (*out.AttemptChange, error) {
		t.Error("the plan ran for an attempt that does not exist")
		return nil, nil
	})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestAttemptRepoConcurrentSubmitsClosingTheSameAttemptSucceedExactlyOnce(t *testing.T) { // the row lock
	e := newAttemptEnv(t)
	a := e.start(t, 3)
	const racers = 8
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = e.attempts.Mutate(ctx, a.ID, func(cur domain.Attempt) (*out.AttemptChange, error) {
				if cur.Status != domain.AttemptInProgress {
					return nil, domain.NewAttemptClosed()
				}
				closed := cur.Submit(cur.StartedAt.Add(time.Minute))
				return &out.AttemptChange{Header: &closed, Items: closed.Items}, nil
			})
		}()
	}
	wg.Wait()

	won := 0
	for _, err := range errs {
		var c *domain.ConflictError
		switch {
		case err == nil:
			won++
		case errors.As(err, &c) && c.Rule == domain.RuleAttemptClosed:
		default:
			t.Errorf("unexpected error %v", err)
		}
	}
	if won != 1 {
		t.Errorf("%d submits succeeded, want exactly 1: the row lock must serialize them", won)
	}
}

// ── ListByExam ──────────────────────────────────────────────────────────────

// closeRaw closes an attempt through SQL, so history tests do not depend on
// Mutate.
func closeRaw(t *testing.T, id, status string, at time.Time, score float64, passed *bool) {
	t.Helper()
	exec(t, `UPDATE attempts SET status = $2, submitted_at = $3, score_percent = $4, passed = $5 WHERE id = $1`, id, status, at, score, passed)
}

func (e *attemptEnv) attemptOfKind(t *testing.T, examID string, cfg domain.SessionConfig, startedAt time.Time) domain.Attempt {
	t.Helper()
	a := e.build(t, e.exam.ID, untimedRandom(2), 3) // the items come from the fixture exam's bank
	a.ExamID, a.Config, a.StartedAt, a.DeadlineAt = examID, cfg, startedAt, nil
	if err := e.attempts.Create(ctx, a); err != nil {
		t.Fatal(err)
	}
	return a
}

var simulationCfg = domain.SessionConfig{Selection: domain.Selection{Kind: domain.SelectSimulation}}

func TestAttemptRepoListByExamReturnsEveryAttemptOfThatExamOnly(t *testing.T) { // slice 06b §1.6
	e := newAttemptEnv(t)
	other := e.add(t, e.subjectID, "AIF-C01")
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	yes := true

	sim := e.attemptOfKind(t, e.exam.ID, simulationCfg, t0)
	closeRaw(t, sim.ID, "submitted", t0.Add(2*time.Hour), 80.5, &yes)
	practice := e.attemptOfKind(t, e.exam.ID, untimedRandom(2), t0.AddDate(0, 0, 1))
	closeRaw(t, practice.ID, "expired", t0.AddDate(0, 0, 1).Add(time.Hour), 10, nil)
	open := e.attemptOfKind(t, e.exam.ID, simulationCfg, t0.AddDate(0, 0, 2))
	e.attemptOfKind(t, other.ID, simulationCfg, t0)

	rows, err := e.attempts.ListByExam(ctx, e.exam.ID)

	if err != nil || len(rows) != 3 {
		t.Fatalf("rows = %d (err %v), want the 3 attempts of this exam", len(rows), err)
	}
	by := map[string]domain.AttemptSummary{}
	for _, r := range rows {
		by[r.ID] = r
	}
	s := by[sim.ID]
	if s.SelectionKind != domain.SelectSimulation || s.TimingKind != domain.Timed || s.Feedback != domain.FeedbackEnd || s.Status != domain.AttemptSubmitted ||
		s.ScorePercent == nil || *s.ScorePercent != 80.5 || s.Passed == nil || !*s.Passed || s.DurationSeconds == nil || *s.DurationSeconds != 7200 || !s.StartedAt.Equal(t0) {
		t.Errorf("simulation row = %+v", s)
	}
	p := by[practice.ID]
	if p.SelectionKind != domain.SelectRandom || p.TimingKind != domain.Untimed || p.Status != domain.AttemptExpired || p.Passed != nil || p.ScorePercent == nil || *p.ScorePercent != 10 {
		t.Errorf("practice row = %+v", p)
	}
	o := by[open.ID]
	if o.Status != domain.AttemptInProgress || o.ScorePercent != nil || o.DurationSeconds != nil || o.SubmittedAt != nil {
		t.Errorf("open row = %+v", o)
	}
}

func TestAttemptRepoListByExamOfAnExamWithoutAttemptsIsEmptyNotAnError(t *testing.T) {
	e := newAttemptEnv(t)
	rows, err := e.attempts.ListByExam(ctx, e.exam.ID)
	if err != nil || len(rows) != 0 {
		t.Errorf("rows = %v, err %v", rows, err)
	}
}

// ── lastSimulation (slice 06b §1.7) ─────────────────────────────────────────

func TestExamSummaryHasNoLastSimulationWithoutAClosedSimulation(t *testing.T) {
	e := newAttemptEnv(t)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	e.attemptOfKind(t, e.exam.ID, simulationCfg, t0) // open, not closed

	got, err := e.exams.GetSummary(ctx, e.exam.ID)

	if err != nil || got.LastSimulation != nil {
		t.Errorf("lastSimulation = %+v (err %v), want none", got.LastSimulation, err)
	}
}

func TestExamSummaryLastSimulationIsTheLatestClosedSimulationOnly(t *testing.T) { // slice 06b §1.7
	e := newAttemptEnv(t)
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	yes, no := true, false

	older := e.attemptOfKind(t, e.exam.ID, simulationCfg, t0)
	closeRaw(t, older.ID, "submitted", t0.Add(2*time.Hour), 60, &no)
	newer := e.attemptOfKind(t, e.exam.ID, simulationCfg, t0.AddDate(0, 0, 3))
	closeRaw(t, newer.ID, "expired", t0.AddDate(0, 0, 3).Add(130*time.Minute), 80.25, &yes)
	practice := e.attemptOfKind(t, e.exam.ID, untimedRandom(2), t0.AddDate(0, 0, 5))
	closeRaw(t, practice.ID, "submitted", t0.AddDate(0, 0, 5).Add(time.Hour), 100, nil)
	e.attemptOfKind(t, e.exam.ID, simulationCfg, t0.AddDate(0, 0, 6)) // open

	want := &domain.SimulationResult{ScorePercent: 80.25, Passed: true, At: t0.AddDate(0, 0, 3).Add(130 * time.Minute)}
	detail, err := e.exams.GetSummary(ctx, e.exam.ID)
	if err != nil || detail.LastSimulation == nil || *detail.LastSimulation != *want {
		t.Errorf("detail lastSimulation = %+v (err %v), want %+v", detail.LastSimulation, err, want)
	}
	list, err := e.exams.ListBySubject(ctx, e.subjectID)
	if err != nil || len(list) != 1 || list[0].LastSimulation == nil || *list[0].LastSimulation != *want {
		t.Errorf("list lastSimulation = %+v (err %v), want %+v", list, err, want)
	}
}

func TestLastSimulationOfOneExamDoesNotLeakIntoAnother(t *testing.T) {
	e := newAttemptEnv(t)
	other := e.add(t, e.subjectID, "AIF-C01")
	yes := true
	t0 := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	a := e.attemptOfKind(t, other.ID, simulationCfg, t0)
	closeRaw(t, a.ID, "submitted", t0.Add(time.Hour), 90, &yes)

	got, _ := e.exams.GetSummary(ctx, e.exam.ID)
	if got.LastSimulation != nil {
		t.Errorf("lastSimulation = %+v, want none for an exam without simulations", got.LastSimulation)
	}
}

// ── NoteDirectory (S06b-6) ──────────────────────────────────────────────────

const summaryKindID = "00000000-0000-7000-8000-000000000001"

func (e *attemptEnv) note(t *testing.T, subjectID, title string) string {
	t.Helper()
	id := uuid.NewString()
	exec(t, `INSERT INTO notes (id, subject_id, kind_id, title) VALUES ($1, $2, $3, $4)`, id, subjectID, summaryKindID, title)
	return id
}

func TestNoteDirectoryFindsAliveNotesOfTheExamsSubjectByExactTitle(t *testing.T) { // S06b-6
	e := newAttemptEnv(t)
	dir := NewNoteDirectory(pool)
	route := e.note(t, e.subjectID, "03 · Route 53")
	vpc := e.note(t, e.subjectID, "04 · VPC")

	got, err := dir.FindNoteIDs(ctx, e.exam.ID, []string{"03 · Route 53", "04 · VPC", "05 · Missing"})

	want := map[string]string{"03 · Route 53": route, "04 · VPC": vpc}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("found %v (err %v), want %v", got, err, want)
	}
}

func TestNoteDirectoryIgnoresDeletedNotesAndNotesOfOtherSubjects(t *testing.T) { // S06b-6
	e := newAttemptEnv(t)
	dir := NewNoteDirectory(pool)
	alive := e.note(t, e.subjectID, "03 · Route 53")
	exec(t, `UPDATE notes SET deleted_at = now() WHERE id = $1`, alive)
	foreign := e.otherSubject(t, "Moto 1")
	e.note(t, foreign, "04 · VPC")

	got, err := dir.FindNoteIDs(ctx, e.exam.ID, []string{"03 · Route 53", "04 · VPC"})

	if err != nil || len(got) != 0 {
		t.Errorf("found %v (err %v), want none: one note is deleted, the other belongs to another subject", got, err)
	}
}

func TestNoteDirectoryMatchesTitlesExactlyIncludingCase(t *testing.T) {
	e := newAttemptEnv(t)
	dir := NewNoteDirectory(pool)
	e.note(t, e.subjectID, "03 · Route 53")

	got, err := dir.FindNoteIDs(ctx, e.exam.ID, []string{"03 · route 53", "03 · Route 53 "})

	if err != nil || len(got) != 0 {
		t.Errorf("found %v (err %v), want an exact, case-sensitive match", got, err)
	}
}

func TestNoteDirectoryAnswersEmptyForNoTitlesOrAnUnknownExam(t *testing.T) {
	e := newAttemptEnv(t)
	dir := NewNoteDirectory(pool)
	e.note(t, e.subjectID, "03 · Route 53")
	if got, err := dir.FindNoteIDs(ctx, e.exam.ID, nil); err != nil || len(got) != 0 {
		t.Errorf("no titles: %v %v", got, err)
	}
	if got, err := dir.FindNoteIDs(ctx, uuid.NewString(), []string{"03 · Route 53"}); err != nil || len(got) != 0 {
		t.Errorf("unknown exam: %v %v", got, err)
	}
}

func TestNoteDirectoryStillResolvesForAnExamDeletedAfterItsAttemptWasTaken(t *testing.T) {
	e := newAttemptEnv(t)
	dir := NewNoteDirectory(pool)
	id := e.note(t, e.subjectID, "03 · Route 53")
	exec(t, `UPDATE questions SET deleted_at = now() WHERE exam_id = $1`, e.exam.ID)
	victim := e.exam
	victim.Delete(time.Now())
	if err := e.exams.Delete(ctx, victim); err != nil {
		t.Fatal(err)
	}

	got, err := dir.FindNoteIDs(ctx, e.exam.ID, []string{"03 · Route 53"})

	if err != nil || got["03 · Route 53"] != id {
		t.Errorf("found %v (err %v), want the note: the review of an old attempt must keep its links", got, err)
	}
}
