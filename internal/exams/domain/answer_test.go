package domain

import (
	"errors"
	"slices"
	"testing"
	"time"
)

func timedCfg(feedback Feedback) SessionConfig {
	return SessionConfig{Selection: Selection{Kind: SelectRandom, Count: 3}, Timing: &Timing{Kind: Timed, Minutes: 10}, Feedback: feedback}
}

func timedAttempt(feedback Feedback, items ...AttemptItem) Attempt {
	a := openAttempt(timedCfg(feedback), 72, items...)
	deadline := startedAt.Add(10 * time.Minute)
	a.DeadlineAt = &deadline
	return a
}

// multiItem has the key B+D (chooseCount 2); the others are single with key B.
func multiItem() AttemptItem {
	return AttemptItem{QuestionID: "qm", Snapshot: keyedSnapshot(true, dom("M"))}
}

func answerNow() time.Time { return startedAt.Add(time.Minute) }

func mustAnswer(t *testing.T, a Attempt, position int, in AnswerInput) (Attempt, AnswerOutcome) {
	t.Helper()
	next, out, err := a.Answer(position, in, answerNow())
	if err != nil {
		t.Fatalf("Answer(%d, %+v): %v", position, in, err)
	}
	return next, out
}

func ruleOf(err error) string {
	var v *ValidationError
	var c *ConflictError
	switch {
	case errors.As(err, &v) && len(v.Violations) > 0:
		return v.Violations[0].Field + ":" + v.Violations[0].Rule
	case errors.As(err, &c):
		return c.Rule
	}
	return ""
}

// ── the deadline and its grace period (AC-19, S06b-1) ───────────────────────

func TestPastDeadlineStartsOnlyAfterDeadlinePlusFiveSeconds(t *testing.T) { // AC-19, BR-A2
	a := timedAttempt(FeedbackEnd, scoredItem(0, nil))
	deadline := *a.DeadlineAt
	for name, tc := range map[string]struct {
		now  time.Time
		want bool
	}{
		"ten seconds before":      {deadline.Add(-10 * time.Second), false},
		"at the deadline":         {deadline, false},
		"three seconds after":     {deadline.Add(3 * time.Second), false},
		"four seconds after":      {deadline.Add(4 * time.Second), false},
		"exactly the grace limit": {deadline.Add(DeadlineGrace), false},
		"a nanosecond past it":    {deadline.Add(DeadlineGrace + time.Nanosecond), true},
		"six seconds after":       {deadline.Add(6 * time.Second), true},
	} {
		if got := a.PastDeadline(tc.now); got != tc.want {
			t.Errorf("%s: PastDeadline = %v, want %v", name, got, tc.want)
		}
	}
}

func TestPastDeadlineIsFalseWithoutADeadlineOrOnceClosed(t *testing.T) {
	untimed := openAttempt(randomCfg, 72, scoredItem(0, nil))
	if untimed.PastDeadline(startedAt.Add(1000 * time.Hour)) {
		t.Error("an untimed attempt never passes a deadline")
	}
	timed := timedAttempt(FeedbackEnd, scoredItem(0, nil)).Submit(startedAt.Add(time.Minute))
	if timed.PastDeadline(startedAt.Add(1000 * time.Hour)) {
		t.Error("a closed attempt is not finalized twice")
	}
}

// ── saving an answer, feedback end (AC-18) ──────────────────────────────────

func TestAnswerWithFeedbackEndStoresTheAnswerAndRevealsNothing(t *testing.T) { // AC-18, BR-A4
	a := timedAttempt(FeedbackEnd, scoredItem(0, nil), scoredItem(1, nil))

	next, out := mustAnswer(t, a, 1, AnswerInput{Selected: []string{"C"}, Flagged: true})

	if out.Position != 1 || !slices.Equal(out.Selected, []string{"C"}) || !out.Flagged || out.Reveal != nil {
		t.Errorf("outcome = %+v, want position 1, [C], flagged, no reveal", out)
	}
	it := next.Items[1]
	if !slices.Equal(it.Selected, []string{"C"}) || !it.Flagged || it.Revealed || it.Correct != nil {
		t.Errorf("stored item = %+v, want selected, flagged, not revealed, no correct", it)
	}
	if it.AnsweredAt == nil || !it.AnsweredAt.Equal(answerNow()) {
		t.Errorf("answeredAt = %v, want %v", it.AnsweredAt, answerNow())
	}
	if len(a.Items[1].Selected) != 0 || a.Items[1].Flagged {
		t.Error("Answer changed its receiver")
	}
}

func TestAnswerCanBeChangedAndClearedUntilTheAttemptCloses(t *testing.T) { // FR-10
	a := openAttempt(randomCfg, 72, scoredItem(0, []string{"A"}))
	a.Items[0].AnsweredAt = &startedAt

	changed, _ := mustAnswer(t, a, 0, AnswerInput{Selected: []string{"B"}})
	cleared, out := mustAnswer(t, changed, 0, AnswerInput{Selected: []string{}, Flagged: true})

	if !slices.Equal(changed.Items[0].Selected, []string{"B"}) {
		t.Errorf("changed = %v", changed.Items[0].Selected)
	}
	if len(cleared.Items[0].Selected) != 0 || cleared.Items[0].AnsweredAt != nil || out.Selected == nil || len(out.Selected) != 0 {
		t.Errorf("cleared item = %+v, outcome selected %#v; want an empty, unanswered item and a non-nil empty selection", cleared.Items[0], out.Selected)
	}
}

// ── validation (S06b-3, AC-20, BR-A5) ───────────────────────────────────────

func TestAnswerRejectsALabelTheItemDoesNotShow(t *testing.T) { // S06b-3
	a := openAttempt(randomCfg, 72, scoredItem(0, nil))
	for name, label := range map[string]string{"Z": "Z", "E on a four-option item": "E", "lower case": "b", "empty": ""} {
		_, _, err := a.Answer(0, AnswerInput{Selected: []string{label}}, answerNow())
		if got := ruleOf(err); got != "selected[0]:invalid_label" {
			t.Errorf("%s: err = %v (%q), want selected[0]:invalid_label", name, err, got)
		}
	}
}

func TestAnswerRejectsARepeatedLabelAsInvalidLabelOnTheSecondOne(t *testing.T) {
	a := openAttempt(randomCfg, 72, multiItem())
	_, _, err := a.Answer(0, AnswerInput{Selected: []string{"B", "B"}}, answerNow())
	if got := ruleOf(err); got != "selected[1]:invalid_label" {
		t.Errorf("err = %v (%q), want selected[1]:invalid_label", err, got)
	}
}

func TestAnswerReportsEveryBadLabelAndNothingElseWhenLabelsAreBad(t *testing.T) {
	a := openAttempt(randomCfg, 72, multiItem())
	_, _, err := a.Answer(0, AnswerInput{Selected: []string{"Z", "B", "Y", "A"}}, answerNow())
	var v *ValidationError
	if !errors.As(err, &v) || !slices.Equal(keysOf(v.Violations), []string{"selected[0]:invalid_label", "selected[2]:invalid_label"}) {
		t.Errorf("err = %v, want invalid_label on selected[0] and selected[2] only", err)
	}
}

func TestAMultiAnswerTakesAtMostChooseCountLabels(t *testing.T) { // AC-20, BR-A5
	a := openAttempt(randomCfg, 72, multiItem()) // B+D: chooseCount 2
	for _, ok := range [][]string{{}, {"B"}, {"B", "D"}, {"D", "B"}, {"A", "C"}} {
		if _, _, err := a.Answer(0, AnswerInput{Selected: ok}, answerNow()); err != nil {
			t.Errorf("selected %v: err = %v, want accepted", ok, err)
		}
	}
	_, _, err := a.Answer(0, AnswerInput{Selected: []string{"A", "B", "D"}}, answerNow())
	if got := ruleOf(err); got != "selected:too_many_selected" {
		t.Errorf("{A,B,D}: err = %v (%q), want selected:too_many_selected", err, got)
	}
}

func TestASingleAnswerTakesAtMostOneLabel(t *testing.T) { // slice 06b §1.3
	a := openAttempt(randomCfg, 72, scoredItem(0, nil))
	_, _, err := a.Answer(0, AnswerInput{Selected: []string{"A", "B"}}, answerNow())
	if got := ruleOf(err); got != "selected:too_many_selected" {
		t.Errorf("err = %v (%q), want selected:too_many_selected", err, got)
	}
}

func TestAnswerToAnUnknownPositionIsNotFound(t *testing.T) {
	a := openAttempt(randomCfg, 72, scoredItem(0, nil))
	for _, position := range []int{-1, 1, 99} {
		if _, _, err := a.Answer(position, AnswerInput{Selected: []string{}}, answerNow()); !errors.Is(err, ErrNotFound) {
			t.Errorf("position %d: err = %v, want ErrNotFound", position, err)
		}
	}
}

func TestAnswerToAClosedAttemptIsAttemptClosed(t *testing.T) { // BR-A3
	closed := openAttempt(randomCfg, 72, scoredItem(0, nil)).Submit(answerNow())
	_, _, err := closed.Answer(0, AnswerInput{Selected: []string{"A"}}, answerNow())
	var c *ConflictError
	if !errors.As(err, &c) || c.Rule != RuleAttemptClosed {
		t.Errorf("err = %v, want conflict attempt_closed", err)
	}
}

// ── feedback each: reveal and lock (S06b-2, D-13) ───────────────────────────

func eachItem() AttemptItem {
	it := scoredItem(0, nil, dom("Seguridad"))
	it.Snapshot.Options[1].Correct, it.Snapshot.Options[2].Correct = false, true // the key is C
	return it
}

func TestAnswerWithFeedbackEachRevealsTheKeyAndLocksTheItem(t *testing.T) { // S06b-2
	a := timedAttempt(FeedbackEach, eachItem())

	next, out := mustAnswer(t, a, 0, AnswerInput{Selected: []string{"B"}})

	r := out.Reveal
	if r == nil || r.Correct || !slices.Equal(r.CorrectLabels, []string{"C"}) || r.Explanation != "why" || r.Rationales["C"] != "because C" || len(r.Rationales) != 4 {
		t.Fatalf("reveal = %+v, want wrong, [C], the explanation and 4 rationales", r)
	}
	it := next.Items[0]
	if !it.Revealed || it.Correct == nil || *it.Correct {
		t.Errorf("stored item = %+v, want revealed with correct=false", it)
	}
}

func TestARevealedAnswerRejectsADifferentSelection(t *testing.T) { // S06b-2, D-13
	revealed, _ := mustAnswer(t, timedAttempt(FeedbackEach, eachItem()), 0, AnswerInput{Selected: []string{"B"}})

	for name, selected := range map[string][]string{"another label": {"C"}, "a cleared selection": {}} {
		_, _, err := revealed.Answer(0, AnswerInput{Selected: selected}, answerNow())
		var c *ConflictError
		if !errors.As(err, &c) || c.Rule != RuleAnswerLocked {
			t.Errorf("%s: err = %v, want conflict answer_locked", name, err)
		}
	}
}

func TestARevealedItemStillTakesAFlagChangeAndKeepsTheReveal(t *testing.T) { // S06b-2
	revealed, _ := mustAnswer(t, timedAttempt(FeedbackEach, eachItem()), 0, AnswerInput{Selected: []string{"B"}})

	next, out := mustAnswer(t, revealed, 0, AnswerInput{Selected: []string{"B"}, Flagged: true})

	if !next.Items[0].Flagged || !slices.Equal(next.Items[0].Selected, []string{"B"}) || !next.Items[0].Revealed {
		t.Errorf("item = %+v, want flagged, still B, still revealed", next.Items[0])
	}
	if out.Reveal == nil || out.Reveal.Correct || !slices.Equal(out.Reveal.CorrectLabels, []string{"C"}) {
		t.Errorf("reveal = %+v, want the same reveal again", out.Reveal)
	}
}

func TestALockedMultiAnswerAcceptsTheSameSetInAnotherOrder(t *testing.T) {
	a := timedAttempt(FeedbackEach, multiItem())
	revealed, _ := mustAnswer(t, a, 0, AnswerInput{Selected: []string{"B", "D"}})
	if _, _, err := revealed.Answer(0, AnswerInput{Selected: []string{"D", "B"}, Flagged: true}, answerNow()); err != nil {
		t.Errorf("same set, other order: err = %v, want accepted", err)
	}
}

func TestFeedbackEachWithAnEmptySelectionRevealsNothingAndLocksNothing(t *testing.T) { // slice 06b §1.3
	a := timedAttempt(FeedbackEach, eachItem())

	next, out := mustAnswer(t, a, 0, AnswerInput{Selected: []string{}, Flagged: true})

	if out.Reveal != nil || next.Items[0].Revealed || next.Items[0].Correct != nil {
		t.Errorf("outcome %+v, item %+v: a flag on an unanswered item reveals nothing", out, next.Items[0])
	}
	if _, out = mustAnswer(t, next, 0, AnswerInput{Selected: []string{"C"}, Flagged: true}); out.Reveal == nil || !out.Reveal.Correct {
		t.Errorf("the first non-empty answer afterwards must reveal: %+v", out.Reveal)
	}
}

func TestRevealOfAnItemFallsBackToTheExactSetCheckWhenCorrectIsNotStored(t *testing.T) {
	it := eachItem()
	it.Selected = []string{"C"}
	if r := it.Reveal(); !r.Correct || !slices.Equal(r.CorrectLabels, []string{"C"}) {
		t.Errorf("reveal = %+v, want correct with [C]", r)
	}
	wrong := false
	it.Correct = &wrong // the stored value wins
	if r := it.Reveal(); r.Correct {
		t.Errorf("reveal = %+v, want the stored false", r)
	}
}

func TestRevealOmitsEmptyRationalesAndKeepsAnEmptyExplanation(t *testing.T) {
	it := scoredItem(0, []string{"B"})
	it.Snapshot.Explanation = ""
	it.Snapshot.Options[0].Rationale, it.Snapshot.Options[2].Rationale = "", ""
	r := it.Reveal()
	if len(r.Rationales) != 2 || r.Rationales["B"] != "because B" || r.Rationales["D"] != "because D" || r.Explanation != "" {
		t.Errorf("reveal = %+v, want the two non-empty rationales and an empty explanation", r)
	}
}

// ── the errors' wire identity ───────────────────────────────────────────────

func TestAttemptConflictsCarryTheirRules(t *testing.T) {
	for rule, err := range map[string]*ConflictError{
		"attempt_closed": NewAttemptClosed(), "attempt_expired": NewAttemptExpired(), "answer_locked": NewAnswerLocked(),
	} {
		if err.Rule != rule || err.Message == "" || err.Field == "" {
			t.Errorf("%s: %+v", rule, err)
		}
	}
}
