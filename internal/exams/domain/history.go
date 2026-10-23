package domain

import "time"

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
func (a Attempt) Summary() AttemptSummary { return AttemptSummary{} }

// NewHistory orders summaries newest first and derives the trend.
func NewHistory(summaries []AttemptSummary) AttemptHistory { return AttemptHistory{} }
