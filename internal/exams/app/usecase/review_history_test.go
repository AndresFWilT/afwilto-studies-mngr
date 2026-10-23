package usecase

import (
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

const (
	routeNoteTitle = "03 · Route 53"
	routeNoteID    = "0abcdef0-0000-7000-8000-0000000b0001"
)

var set3Config = domain.SessionConfig{
	Selection: domain.Selection{Kind: domain.SelectOriginSet, Set: "Timed Set 3"},
	Timing:    &domain.Timing{Kind: domain.Untimed}, Feedback: domain.FeedbackEnd,
}

// addSet3 puts three unclassified questions numbered 1..3 into the bank: the
// first points at "03 · Route 53 (diap. 12)", the second at a note that does
// not exist, the third has an origin without a noteRef.
func (e sessionEnv) addSet3(t *testing.T) {
	t.Helper()
	bank := e.bank.Bank(e.examID)
	for i, ref := range []string{"03 · Route 53 (diap. 12)", "No such note (diap. 1)", ""} {
		q := attemptQuestion("ts3-" + string(rune('a'+i)))
		q.Origin = &domain.Origin{Set: "Timed Set 3", Number: i + 1, NoteRef: ref}
		bank.Questions = append(bank.Questions, q)
	}
	e.bank.Set(e.examID, bank)
	e.notes.Add(e.examID, routeNoteTitle, routeNoteID)
}

func (e sessionEnv) closedSet3(t *testing.T) string {
	t.Helper()
	e.addSet3(t)
	view := e.begin(t, set3Config, nil)
	e.save(t, view.ID, 0, true, e.keyOf(view.ID, 0)...)
	e.save(t, view.ID, 1, false, e.wrongOf(view.ID, 1))
	if _, err := e.submit.Execute(ctx, view.ID); err != nil {
		t.Fatal(err)
	}
	return view.ID
}

func TestReviewOfAnOpenAttemptIsAttemptInProgressWithItsId(t *testing.T) { // slice 06b §1.5
	e := newSessionEnv(t)
	view := e.begin(t, randomUntimed(2), nil)

	_, err := e.review.Execute(ctx, view.ID)

	wantConflict(t, err, domain.RuleAttemptInProgress)
	if c := err.(*domain.ConflictError); c.Field != "attemptId" || !strings.Contains(c.Message, view.ID) {
		t.Errorf("conflict = %+v, want attemptId and %s", c, view.ID)
	}
}

func TestReviewListsEveryItemWithItsKeySelectionAndFlag(t *testing.T) { // FR-10 Review
	e := newSessionEnv(t)
	id := e.closedSet3(t)

	items, err := e.review.Execute(ctx, id)

	if err != nil || len(items) != 3 {
		t.Fatalf("items = %d, err %v", len(items), err)
	}
	first, second, third := items[0], items[1], items[2]
	if first.Item.Position != 0 || !first.Reveal.Correct || !first.Item.Flagged || !slices.Equal(first.Reveal.CorrectLabels, e.keyOf(id, 0)) ||
		first.Reveal.Explanation == "" || len(first.Reveal.Rationales) != 4 {
		t.Errorf("first = %+v", first)
	}
	if second.Reveal.Correct || !slices.Equal(second.Item.Selected, []string{e.wrongOf(id, 1)}) {
		t.Errorf("second = %+v, want wrong with the wrong label kept", second)
	}
	if third.Reveal.Correct || len(third.Item.Selected) != 0 {
		t.Errorf("third = %+v, want an unanswered item marked wrong", third)
	}
}

func TestReviewLinksTheNoteWhoseTitleIsTheNoteRefWithoutItsSlideReference(t *testing.T) { // S06b-6
	e := newSessionEnv(t)
	id := e.closedSet3(t)

	items, err := e.review.Execute(ctx, id)

	if err != nil || items[0].NoteID != routeNoteID {
		t.Fatalf("noteId = %q (err %v), want %s", items[0].NoteID, err, routeNoteID)
	}
	if items[1].NoteID != "" || items[2].NoteID != "" {
		t.Errorf("noteIds = %q, %q; want none for a missing note and for no noteRef", items[1].NoteID, items[2].NoteID)
	}
}

func TestReviewDropsTheNoteLinkOnceTheNoteIsDeleted(t *testing.T) { // S06b-6
	e := newSessionEnv(t)
	id := e.closedSet3(t)
	e.notes.Delete(e.examID, routeNoteTitle)

	items, err := e.review.Execute(ctx, id)

	if err != nil || items[0].NoteID != "" {
		t.Errorf("noteId = %q (err %v), want none after the note was deleted", items[0].NoteID, err)
	}
}

func TestReviewSurfacesAFailingNoteLookupInsteadOfHidingTheLinks(t *testing.T) {
	e := newSessionEnv(t)
	id := e.closedSet3(t)
	e.notes.Err = errors.New("notes table unreachable")

	if _, err := e.review.Execute(ctx, id); err == nil {
		t.Error("a failed lookup was swallowed: the review would silently lose its links")
	}
}

func TestReviewOfAnAttemptPastItsDeadlineFinalizesItFirst(t *testing.T) { // every operation starts with finalizeIfExpired
	e := newSessionEnv(t)
	view := e.begin(t, timedRandom(2, 5, domain.FeedbackEnd), nil)
	e.save(t, view.ID, 0, false, e.keyOf(view.ID, 0)...)
	e.at(5*time.Minute + 6*time.Second)

	items, err := e.review.Execute(ctx, view.ID)

	if err != nil || len(items) != 2 || !items[0].Reveal.Correct {
		t.Fatalf("items = %+v, err %v", items, err)
	}
	if e.attempts.Rows[view.ID].Status != domain.AttemptExpired {
		t.Errorf("status = %q, want expired", e.attempts.Rows[view.ID].Status)
	}
}

func TestReviewOfAnUnknownAttemptIsNotFound(t *testing.T) {
	e := newSessionEnv(t)
	if _, err := e.review.Execute(ctx, "0abcdef0-0000-7000-8000-ffffffffffff"); !isNotFound(err) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// ── history ─────────────────────────────────────────────────────────────────

func (e sessionEnv) finish(t *testing.T, cfg domain.SessionConfig, right int) string {
	t.Helper()
	view := e.begin(t, cfg, nil)
	e.answerMix(t, view.ID, right, 0)
	e.clock.Advance(30 * time.Minute)
	if _, err := e.submit.Execute(ctx, view.ID); err != nil {
		t.Fatal(err)
	}
	return view.ID
}

func TestHistoryHasThreeAttemptsNewestFirstAndATrendOfTheTwoSimulations(t *testing.T) { // S06b-7, D-11
	e := newSessionEnv(t)
	sim := domain.SessionConfig{Selection: domain.Selection{Kind: domain.SelectSimulation}}
	first := e.finish(t, sim, 65)
	e.clock.Advance(24 * time.Hour)
	practice := e.finish(t, randomUntimed(3), 1)
	e.clock.Advance(24 * time.Hour)
	last := e.finish(t, sim, 0)

	h, err := e.history.Execute(ctx, e.examID)

	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, a := range h.Attempts {
		ids = append(ids, a.ID)
	}
	if !slices.Equal(ids, []string{last, practice, first}) {
		t.Errorf("order = %v, want newest first %v", ids, []string{last, practice, first})
	}
	if len(h.Trend) != 2 || h.Trend[0].ScorePercent != 100 || h.Trend[1].ScorePercent != 0 || !h.Trend[0].At.Before(h.Trend[1].At) {
		t.Errorf("trend = %+v, want the two simulations, 100 then 0, oldest first", h.Trend)
	}
	if h.Attempts[1].SelectionKind != domain.SelectRandom || h.Attempts[1].Passed != nil || h.Attempts[0].Passed == nil || *h.Attempts[0].Passed {
		t.Errorf("rows = %+v", h.Attempts)
	}
}

func TestHistoryListsAnOpenAttemptWithoutFinalizingIt(t *testing.T) {
	e := newSessionEnv(t)
	view := e.begin(t, timedRandom(2, 5, domain.FeedbackEnd), nil)
	e.at(time.Hour)

	h, err := e.history.Execute(ctx, e.examID)

	if err != nil || len(h.Attempts) != 1 || h.Attempts[0].Status != domain.AttemptInProgress || len(h.Trend) != 0 {
		t.Errorf("history = %+v, err %v", h, err)
	}
	if e.attempts.Rows[view.ID].Status != domain.AttemptInProgress {
		t.Error("the history finalized an attempt: it is a pure read")
	}
}

func TestHistoryOfAnExamWithoutAttemptsIsEmptyAndOfAnUnknownExamNotFound(t *testing.T) {
	e := newSessionEnv(t)
	h, err := e.history.Execute(ctx, e.examID)
	if err != nil || len(h.Attempts) != 0 || h.Attempts == nil || h.Trend == nil {
		t.Errorf("history = %#v, err %v; want empty lists", h, err)
	}
	if _, err := e.history.Execute(ctx, "0abcdef0-0000-7000-8000-ffffffffffff"); !isNotFound(err) {
		t.Errorf("unknown exam: err = %v, want ErrNotFound", err)
	}
}

// ── decision 3: start finalizes an expired open attempt ────────────────────

func TestStartFinalizesAnOpenAttemptPastItsGraceAndStartsTheNewOne(t *testing.T) { // slice 06b decision 3
	e := newSessionEnv(t)
	old := e.begin(t, timedRandom(4, 10, domain.FeedbackEnd), nil)
	e.save(t, old.ID, 0, false, e.keyOf(old.ID, 0)...)
	e.at(10*time.Minute + 6*time.Second)

	fresh, err := e.start.Execute(ctx, e.examID, in.StartAttemptCommand{Config: randomUntimed(2)})

	if err != nil || fresh.ID == old.ID || fresh.Status != domain.AttemptInProgress {
		t.Fatalf("start = %+v, err %v; want a new open attempt", fresh.Attempt.ID, err)
	}
	stored := e.attempts.Rows[old.ID]
	if stored.Status != domain.AttemptExpired || stored.ScorePercent == nil || *stored.ScorePercent != 25 {
		t.Errorf("old = %q, score %v; want expired and scored 25", stored.Status, stored.ScorePercent)
	}
}

func TestStartStillConflictsInsideTheGracePeriodAndForUntimedAttempts(t *testing.T) { // BR-A1 with decision 3
	e := newSessionEnv(t)
	timed := e.begin(t, timedRandom(2, 10, domain.FeedbackEnd), nil)
	e.at(10*time.Minute + 5*time.Second) // exactly the grace limit: not past it

	_, err := e.start.Execute(ctx, e.examID, in.StartAttemptCommand{Config: randomUntimed(2)})
	wantConflict(t, err, domain.RuleAttemptInProgress)
	if e.attempts.Rows[timed.ID].Status != domain.AttemptInProgress {
		t.Error("an attempt inside its grace was finalized")
	}

	e2 := newSessionEnv(t)
	e2.begin(t, randomUntimed(2), nil)
	e2.at(1000 * time.Hour)
	_, err = e2.start.Execute(ctx, e2.examID, in.StartAttemptCommand{Config: randomUntimed(2)})
	wantConflict(t, err, domain.RuleAttemptInProgress)
}

func TestARejectedStartConfigDoesNotFinalizeTheExpiredAttempt(t *testing.T) {
	e := newSessionEnv(t)
	old := e.begin(t, timedRandom(2, 10, domain.FeedbackEnd), nil)
	e.at(time.Hour)
	bad := randomUntimed(2)
	bad.Selection.Count = 0

	_, err := e.start.Execute(ctx, e.examID, in.StartAttemptCommand{Config: bad})

	wantViolation(t, err, "config.selection.count", domain.RuleRange)
	if e.attempts.Rows[old.ID].Status != domain.AttemptInProgress {
		t.Error("a start that fails validation must not touch the open attempt")
	}
}
