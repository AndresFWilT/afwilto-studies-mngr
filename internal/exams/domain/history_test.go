package domain

import (
	"slices"
	"testing"
	"time"
)

func closedAt(a Attempt, start time.Time, minutes int, answered ...string) Attempt {
	a.StartedAt = start
	for i := range a.Items {
		a.Items[i].Selected = []string{answered[i]}
	}
	return a.Submit(start.Add(time.Duration(minutes) * time.Minute))
}

func TestSummaryOfAnOpenSimulationIsTimedWithFeedbackEndAndNoScore(t *testing.T) { // slice 06b §5
	a := openAttempt(simulation(), 72, scoredItem(0, nil))

	s := a.Summary()

	if s.ID != "att" || s.SelectionKind != SelectSimulation || s.TimingKind != Timed || s.Feedback != FeedbackEnd || s.Status != AttemptInProgress || !s.StartedAt.Equal(startedAt) {
		t.Errorf("summary = %+v", s)
	}
	if s.SubmittedAt != nil || s.DurationSeconds != nil || s.ScorePercent != nil || s.Passed != nil {
		t.Errorf("an open attempt has no outcome yet: %+v", s)
	}
}

func TestSummaryOfAClosedPracticeAttemptCarriesScoreAndDurationButNoPassed(t *testing.T) {
	cfg := practice(Selection{Kind: SelectRandom, Count: 2})
	cfg.Feedback = FeedbackEach
	a := closedAt(openAttempt(cfg, 72, scoredItem(0, nil), scoredItem(1, nil)), startedAt, 12, "B", "A")

	s := a.Summary()

	if s.SelectionKind != SelectRandom || s.TimingKind != Untimed || s.Feedback != FeedbackEach || s.Status != AttemptSubmitted {
		t.Errorf("summary = %+v", s)
	}
	if s.ScorePercent == nil || *s.ScorePercent != 50 || s.DurationSeconds == nil || *s.DurationSeconds != 720 || s.Passed != nil {
		t.Errorf("score %v, duration %v, passed %v; want 50, 720, none", s.ScorePercent, s.DurationSeconds, s.Passed)
	}
	if s.SubmittedAt == nil || !s.SubmittedAt.Equal(startedAt.Add(12*time.Minute)) {
		t.Errorf("submittedAt = %v", s.SubmittedAt)
	}
}

func TestSummaryOfATimedPracticeAttemptIsTimed(t *testing.T) {
	a := timedAttempt(FeedbackEnd, scoredItem(0, nil))
	if s := a.Summary(); s.TimingKind != Timed {
		t.Errorf("timingKind = %q, want timed", s.TimingKind)
	}
}

func TestHistoryListsThreeAttemptsNewestFirstAndTrendsTheTwoSimulationsOldestFirst(t *testing.T) { // S06b-7, D-11
	day := func(d int) time.Time { return startedAt.AddDate(0, 0, d) }
	sim1 := closedAt(openAttempt(simulation(), 72, scoredItem(0, nil)), day(0), 60, "A")
	sim1.ID = "sim-1"
	practice1 := closedAt(openAttempt(randomCfg, 72, scoredItem(0, nil)), day(1), 10, "B")
	practice1.ID = "practice"
	sim2 := closedAt(openAttempt(simulation(), 72, scoredItem(0, nil)), day(2), 90, "B")
	sim2.ID = "sim-2"

	h := NewHistory([]AttemptSummary{sim2.Summary(), sim1.Summary(), practice1.Summary()})

	var ids []string
	for _, s := range h.Attempts {
		ids = append(ids, s.ID)
	}
	if !slices.Equal(ids, []string{"sim-2", "practice", "sim-1"}) {
		t.Errorf("order = %v, want newest first", ids)
	}
	want := []TrendPoint{{At: day(0).Add(60 * time.Minute), ScorePercent: 0}, {At: day(2).Add(90 * time.Minute), ScorePercent: 100}}
	if len(h.Trend) != 2 || h.Trend[0] != want[0] || h.Trend[1] != want[1] {
		t.Errorf("trend = %+v, want %+v", h.Trend, want)
	}
}

func TestTrendLeavesOutOpenAttemptsButKeepsExpiredSimulations(t *testing.T) {
	deadline := startedAt.Add(130 * time.Minute)
	expired := openAttempt(simulation(), 72, scoredItem(0, nil))
	expired.DeadlineAt = &deadline
	expired = expired.Expire()
	open := openAttempt(simulation(), 72, scoredItem(0, nil))
	open.StartedAt = startedAt.AddDate(0, 0, 1)

	h := NewHistory([]AttemptSummary{open.Summary(), expired.Summary()})

	if len(h.Attempts) != 2 || len(h.Trend) != 1 || !h.Trend[0].At.Equal(deadline) {
		t.Errorf("history = %+v, want 2 attempts and a single trend point at the deadline", h)
	}
}

func TestEmptyHistoryHasEmptyNotNilLists(t *testing.T) {
	h := NewHistory(nil)
	if h.Attempts == nil || h.Trend == nil || len(h.Attempts) != 0 || len(h.Trend) != 0 {
		t.Errorf("history = %#v, want empty non-nil lists (the wire shows [])", h)
	}
}

func TestHistoryBreaksStartTimeTiesByIDDescendingSoTheOrderIsTotal(t *testing.T) {
	mk := func(id string) AttemptSummary {
		a := openAttempt(randomCfg, 72, scoredItem(0, nil))
		a.ID = id
		return a.Summary()
	}
	h := NewHistory([]AttemptSummary{mk("a"), mk("c"), mk("b")})
	var ids []string
	for _, s := range h.Attempts {
		ids = append(ids, s.ID)
	}
	if !slices.Equal(ids, []string{"c", "b", "a"}) {
		t.Errorf("order = %v, want [c b a]", ids)
	}
}
