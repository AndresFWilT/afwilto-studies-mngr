package domain

import (
	"errors"
	"time"
)

// CategoryScore is one bar of the breakdown.
type CategoryScore struct {
	Name    string
	Correct int
	Total   int
	Percent float64
}

// ClassificationScore groups the category scores of one classification,
// weakest first.
type ClassificationScore struct {
	Classification string
	Categories     []CategoryScore
}

// AttemptResult is what submit returns and a closed attempt's GET repeats.
type AttemptResult struct {
	ScorePercent    float64
	Passed          *bool
	CorrectCount    int
	Total           int
	DurationSeconds int
	Breakdown       []ClassificationScore
}

var errNotImplemented = errors.New("not implemented")

// Percent2 is 100 × correct / total rounded half up to two decimals.
func Percent2(correct, total int) float64 { return -1 }

// CorrectLabels are the labels of the snapshot's correct options, in order.
func (s ItemSnapshot) CorrectLabels() []string { return nil }

// IsCorrect holds when the selected labels are exactly the correct ones.
func (s ItemSnapshot) IsCorrect(selected []string) bool { return false }

// Close scores the attempt as it stands and closes it with the given status at
// the given instant.
func (a Attempt) Close(status AttemptStatus, at time.Time) Attempt { return a }

// Submit closes the attempt as submitted.
func (a Attempt) Submit(now time.Time) Attempt { return a }

// Expire closes the attempt as expired, at its deadline.
func (a Attempt) Expire() Attempt { return a }

// Result is the score of a closed attempt.
func (a Attempt) Result() AttemptResult { return AttemptResult{} }

// NoteTitleFromRef is the note title an origin.noteRef points at.
func NoteTitleFromRef(ref string) string { return ref }
