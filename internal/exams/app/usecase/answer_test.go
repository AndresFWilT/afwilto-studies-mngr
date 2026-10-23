package usecase

import (
	"slices"
	"testing"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// ── S06b-1: lazy expiry through the Clock port ──────────────────────────────

func TestGetCountsDownThenHoldsAtZeroThroughTheGraceThenExpiresWithAScore(t *testing.T) { // S06b-1
	e := newSessionEnv(t)
	view := e.begin(t, timedRandom(4, 10, domain.FeedbackEnd), nil) // deadlineAt = T = t0 + 10 min
	e.save(t, view.ID, 0, false, e.keyOf(view.ID, 0)...)

	e.at(10*time.Minute - 10*time.Second)
	if v := e.read(t, view.ID); v.Status != domain.AttemptInProgress || v.RemainingSeconds == nil || *v.RemainingSeconds != 10 {
		t.Errorf("T-10 s: status %q, remaining %v; want in_progress and 10", v.Status, v.RemainingSeconds)
	}

	e.at(10*time.Minute + 3*time.Second)
	if v := e.read(t, view.ID); v.Status != domain.AttemptInProgress || v.RemainingSeconds == nil || *v.RemainingSeconds != 0 {
		t.Errorf("T+3 s: status %q, remaining %v; want in_progress and 0", v.Status, v.RemainingSeconds)
	}

	e.at(10*time.Minute + 6*time.Second)
	v := e.read(t, view.ID)
	if v.Status != domain.AttemptExpired || v.Result == nil || v.Result.ScorePercent != 25 || v.Result.CorrectCount != 1 || v.Result.Total != 4 {
		t.Fatalf("T+6 s: status %q, result %+v; want expired and 25%% (1 of 4)", v.Status, v.Result)
	}
	if v.RemainingSeconds == nil || *v.RemainingSeconds != 0 {
		t.Errorf("remaining of a closed timed attempt = %v, want 0", v.RemainingSeconds)
	}
	stored := e.attempts.Rows[view.ID]
	if stored.Status != domain.AttemptExpired || stored.ScorePercent == nil || *stored.ScorePercent != 25 ||
		stored.SubmittedAt == nil || !stored.SubmittedAt.Equal(t0.Add(10*time.Minute)) {
		t.Errorf("stored = status %q, score %v, submittedAt %v; want expired, 25, at the deadline", stored.Status, stored.ScorePercent, stored.SubmittedAt)
	}
}

func TestGetOfAnUntimedAttemptHasNoRemainingTimeAndNeverExpires(t *testing.T) {
	e := newSessionEnv(t)
	view := e.begin(t, randomUntimed(3), nil)
	e.at(1000 * time.Hour)

	v := e.read(t, view.ID)

	if v.Status != domain.AttemptInProgress || v.RemainingSeconds != nil || v.Result != nil {
		t.Errorf("status %q, remaining %v, result %v; want an open attempt with neither", v.Status, v.RemainingSeconds, v.Result)
	}
}

func TestGetOfAnOpenAttemptReturnsTheAnswerStateAndNoResult(t *testing.T) {
	e := newSessionEnv(t)
	view := e.begin(t, randomUntimed(3), nil)
	e.save(t, view.ID, 1, true, "C")

	v := e.read(t, view.ID)

	if !slices.Equal(v.Items[1].Selected, []string{"C"}) || !v.Items[1].Flagged || v.Items[0].Flagged || v.Result != nil {
		t.Errorf("items = %+v, result %v", v.Items, v.Result)
	}
}

func TestGetOfASubmittedAttemptCarriesTheSameResultTheSubmitReturned(t *testing.T) { // slice 06b decision 1
	e := newSessionEnv(t)
	view := e.begin(t, timedRandom(4, 30, domain.FeedbackEnd), nil)
	e.save(t, view.ID, 0, false, e.keyOf(view.ID, 0)...)
	e.at(5 * time.Minute)
	submitted, err := e.submit.Execute(ctx, view.ID)
	if err != nil {
		t.Fatal(err)
	}

	v := e.read(t, view.ID)

	if v.Status != domain.AttemptSubmitted || v.Result == nil || v.Result.ScorePercent != submitted.ScorePercent ||
		v.Result.CorrectCount != submitted.CorrectCount || v.Result.DurationSeconds != 300 || submitted.DurationSeconds != 300 {
		t.Errorf("get result %+v vs submit result %+v", v.Result, submitted)
	}
}

func TestGetOfAnUnknownAttemptIsNotFound(t *testing.T) {
	e := newSessionEnv(t)
	if _, err := e.get.Execute(ctx, "0abcdef0-0000-7000-8000-ffffffffffff"); !isNotFound(err) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

// ── AC-19: a write inside the grace is accepted, one past it is not ─────────

func TestAWriteFourSecondsPastTheDeadlineIsAcceptedAndOneSixSecondsPastIsExpired(t *testing.T) { // AC-19
	e := newSessionEnv(t)
	view := e.begin(t, timedRandom(4, 10, domain.FeedbackEnd), nil)

	e.at(10*time.Minute + 4*time.Second)
	e.save(t, view.ID, 0, false, e.keyOf(view.ID, 0)...)

	e.at(10*time.Minute + 6*time.Second)
	_, err := e.answer.Execute(ctx, view.ID, 1, domain.AnswerInput{Selected: e.keyOf(view.ID, 1)})
	wantConflict(t, err, domain.RuleAttemptExpired)

	stored := e.attempts.Rows[view.ID]
	if stored.Status != domain.AttemptExpired || stored.ScorePercent == nil || *stored.ScorePercent != 25 {
		t.Errorf("stored = %q, score %v; want expired and scored 25 on the answer that landed in time", stored.Status, stored.ScorePercent)
	}
	if len(stored.Items[1].Selected) != 0 {
		t.Errorf("the late write was applied: %v", stored.Items[1].Selected)
	}
}

func TestAWriteAfterTheExpiryWasFinalizedIsAttemptClosed(t *testing.T) { // BR-A2 vs BR-A3
	e := newSessionEnv(t)
	view := e.begin(t, timedRandom(2, 1, domain.FeedbackEnd), nil)
	e.at(time.Minute + 6*time.Second)
	_, first := e.answer.Execute(ctx, view.ID, 0, domain.AnswerInput{Selected: []string{"A"}})
	wantConflict(t, first, domain.RuleAttemptExpired)

	_, second := e.answer.Execute(ctx, view.ID, 0, domain.AnswerInput{Selected: []string{"A"}})

	wantConflict(t, second, domain.RuleAttemptClosed)
}

func TestAWriteAfterASubmitIsAttemptClosed(t *testing.T) { // BR-A3
	e := newSessionEnv(t)
	view := e.begin(t, randomUntimed(2), nil)
	if _, err := e.submit.Execute(ctx, view.ID); err != nil {
		t.Fatal(err)
	}

	_, err := e.answer.Execute(ctx, view.ID, 0, domain.AnswerInput{Selected: []string{"A"}})

	wantConflict(t, err, domain.RuleAttemptClosed)
}

// ── answering ───────────────────────────────────────────────────────────────

func TestAnswerStoresTheSelectionAndLeavesTheOtherItemsAlone(t *testing.T) {
	e := newSessionEnv(t)
	view := e.begin(t, randomUntimed(3), nil)
	e.at(2 * time.Minute)

	out := e.save(t, view.ID, 1, true, "C")

	stored := e.attempts.Rows[view.ID].Items
	if !slices.Equal(stored[1].Selected, []string{"C"}) || !stored[1].Flagged || stored[1].AnsweredAt == nil || !stored[1].AnsweredAt.Equal(t0.Add(2*time.Minute)) {
		t.Errorf("item 1 = %+v", stored[1])
	}
	if len(stored[0].Selected) != 0 || len(stored[2].Selected) != 0 || stored[0].Flagged {
		t.Errorf("neighbours changed: %+v / %+v", stored[0], stored[2])
	}
	if out.Position != 1 || !slices.Equal(out.Selected, []string{"C"}) || !out.Flagged {
		t.Errorf("outcome = %+v", out)
	}
}

func TestAnswerWithFeedbackEndRevealsNothingAndStoresNoCorrectness(t *testing.T) { // AC-18, BR-A4
	e := newSessionEnv(t)
	view := e.begin(t, randomUntimed(2), nil)

	out := e.save(t, view.ID, 0, false, e.keyOf(view.ID, 0)...)

	if out.Reveal != nil {
		t.Errorf("reveal = %+v, want none before the attempt closes", out.Reveal)
	}
	if it := e.attempts.Rows[view.ID].Items[0]; it.Revealed || it.Correct != nil {
		t.Errorf("stored item = %+v, want no reveal state", it)
	}
}

func TestAnswerReturnsTheServerClockOnTimedAttemptsOnly(t *testing.T) { // slice 06b decision 2
	e := newSessionEnv(t)
	timed := e.begin(t, timedRandom(2, 10, domain.FeedbackEnd), nil)
	e.at(time.Minute)

	if out := e.save(t, timed.ID, 0, false, "A"); out.RemainingSeconds == nil || *out.RemainingSeconds != 540 {
		t.Errorf("timed: remaining = %v, want 540", out.RemainingSeconds)
	}
	e.at(10*time.Minute + 3*time.Second) // inside the grace
	if out := e.save(t, timed.ID, 0, false, "B"); out.RemainingSeconds == nil || *out.RemainingSeconds != 0 {
		t.Errorf("in the grace: remaining = %v, want 0", out.RemainingSeconds)
	}

	e2 := newSessionEnv(t)
	untimed := e2.begin(t, randomUntimed(2), nil)
	if out := e2.save(t, untimed.ID, 0, false, "A"); out.RemainingSeconds != nil {
		t.Errorf("untimed: remaining = %v, want none", *out.RemainingSeconds)
	}
}

func TestFeedbackEachRevealsAWrongAnswerAndLocksItButStillTakesTheFlag(t *testing.T) { // S06b-2
	e := newSessionEnv(t)
	view := e.begin(t, untimedEach(2), nil)
	key, wrong := e.keyOf(view.ID, 0), e.wrongOf(view.ID, 0)

	out := e.save(t, view.ID, 0, false, wrong)
	if out.Reveal == nil || out.Reveal.Correct || !slices.Equal(out.Reveal.CorrectLabels, key) || len(out.Reveal.Rationales) != 4 {
		t.Fatalf("reveal = %+v, want wrong with correctLabels %v", out.Reveal, key)
	}

	_, err := e.answer.Execute(ctx, view.ID, 0, domain.AnswerInput{Selected: key})
	wantConflict(t, err, domain.RuleAnswerLocked)

	out = e.save(t, view.ID, 0, true, wrong)
	if !out.Flagged || out.Reveal == nil || out.Reveal.Correct {
		t.Errorf("flag-only change: %+v, want flagged with the reveal repeated", out)
	}
	stored := e.attempts.Rows[view.ID].Items[0]
	if !slices.Equal(stored.Selected, []string{wrong}) || !stored.Flagged || !stored.Revealed {
		t.Errorf("stored = %+v, want the original wrong answer, flagged, revealed", stored)
	}
}

func TestFeedbackEachRevealsARightAnswerAsCorrect(t *testing.T) {
	e := newSessionEnv(t)
	view := e.begin(t, untimedEach(1), nil)

	out := e.save(t, view.ID, 0, false, e.keyOf(view.ID, 0)...)

	if out.Reveal == nil || !out.Reveal.Correct {
		t.Errorf("reveal = %+v, want correct", out.Reveal)
	}
}

func TestAnInvalidLabelIsRejectedAndNothingIsStored(t *testing.T) { // S06b-3
	e := newSessionEnv(t)
	view := e.begin(t, randomUntimed(2), nil)
	writes := e.attempts.Writes

	_, err := e.answer.Execute(ctx, view.ID, 0, domain.AnswerInput{Selected: []string{"Z"}, Flagged: true})

	wantViolation(t, err, "selected[0]", domain.RuleInvalidLabel)
	if e.attempts.Writes != writes || e.attempts.Rows[view.ID].Items[0].Flagged {
		t.Error("a rejected answer changed the attempt")
	}
}

func TestAnswerToAnUnknownAttemptOrPositionIsNotFound(t *testing.T) {
	e := newSessionEnv(t)
	view := e.begin(t, randomUntimed(2), nil)
	if _, err := e.answer.Execute(ctx, "0abcdef0-0000-7000-8000-ffffffffffff", 0, domain.AnswerInput{Selected: []string{}}); !isNotFound(err) {
		t.Errorf("unknown attempt: %v", err)
	}
	if _, err := e.answer.Execute(ctx, view.ID, 2, domain.AnswerInput{Selected: []string{}}); !isNotFound(err) {
		t.Errorf("unknown position: %v", err)
	}
}
