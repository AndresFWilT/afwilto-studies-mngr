package domain

import (
	"slices"
	"strings"
	"time"
)

// AttemptSummary is one row of an exam's history.
type AttemptSummary struct {
	ID              string
	SelectionKind   SelectionKind
	TimingKind      TimingKind
	Feedback        Feedback
	Status          AttemptStatus
	StartedAt       time.Time
	SubmittedAt     *time.Time
	DurationSeconds *int
	ScorePercent    *float64
	Passed          *bool
}

// TrendPoint is one point of the simulation trend line (D-11).
type TrendPoint struct {
	At           time.Time
	ScorePercent float64
}

// AttemptHistory is the history of an exam: its attempts newest first and the
// trend of the closed simulations, oldest first.
type AttemptHistory struct {
	Attempts []AttemptSummary
	Trend    []TrendPoint
}

// Summary is the history row of an attempt. A simulation is forced to timed
// and feedback end, so its summary says so although its config omits both.
// Score, duration and passed show once the attempt is closed.
func (a Attempt) Summary() AttemptSummary {
	s := AttemptSummary{
		ID: a.ID, SelectionKind: a.Config.Selection.Kind, TimingKind: Untimed, Feedback: a.Config.EffectiveFeedback(),
		Status: a.Status, StartedAt: a.StartedAt,
	}
	if a.Config.Selection.Kind == SelectSimulation || (a.Config.Timing != nil && a.Config.Timing.Kind == Timed) {
		s.TimingKind = Timed
	}
	if a.Status != AttemptInProgress {
		s.SubmittedAt, s.ScorePercent, s.Passed = a.SubmittedAt, a.ScorePercent, a.Passed
		if a.SubmittedAt != nil {
			seconds := int(a.SubmittedAt.Sub(a.StartedAt) / time.Second)
			s.DurationSeconds = &seconds
		}
	}
	return s
}

// NewHistory orders summaries newest first (ties by id, descending, so the
// order is total) and derives the trend: the closed simulations, oldest first
// by the time they closed (D-11).
func NewHistory(summaries []AttemptSummary) AttemptHistory {
	h := AttemptHistory{Attempts: slices.Clone(summaries), Trend: []TrendPoint{}}
	if h.Attempts == nil {
		h.Attempts = []AttemptSummary{}
	}
	slices.SortFunc(h.Attempts, func(x, y AttemptSummary) int {
		if c := y.StartedAt.Compare(x.StartedAt); c != 0 {
			return c
		}
		return strings.Compare(y.ID, x.ID)
	})
	for _, s := range h.Attempts {
		if s.SelectionKind == SelectSimulation && s.Status != AttemptInProgress && s.SubmittedAt != nil && s.ScorePercent != nil {
			h.Trend = append(h.Trend, TrendPoint{At: *s.SubmittedAt, ScorePercent: *s.ScorePercent})
		}
	}
	slices.SortStableFunc(h.Trend, func(x, y TrendPoint) int { return x.At.Compare(y.At) })
	return h
}
