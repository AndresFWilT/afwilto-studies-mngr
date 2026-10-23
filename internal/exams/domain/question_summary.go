package domain

import (
	"errors"
	"time"
)

// QuestionSummary is a row of the bank browser (Spec 18 slice 04 §1.1).
type QuestionSummary struct {
	ID          string
	Key         string
	Type        QuestionType
	StemExcerpt string
	Categories  []CategoryRef
	Difficulty  string // "" when absent
	Origin      *Origin
	Version     int
	UpdatedAt   time.Time
}

// QuestionFilter holds the bank browser filters, combined with AND. An empty
// field is no filter. Unclassified keeps the questions with no category in the
// primary classification.
type QuestionFilter struct {
	CategoryID   string
	Origin       string
	Difficulty   string
	Text         string
	Unclassified bool
}

// StemExcerpt is the first 200 characters of a stem with Markdown syntax stripped.
func StemExcerpt(stem string) string { return "" }

// SummarizeQuestion builds the browser row of a stored question.
func SummarizeQuestion(q Question) QuestionSummary { return QuestionSummary{} }

// Violations reports the filter values that are not allowed.
func (f QuestionFilter) Violations() []Violation { return nil }

var errNotImplemented = errors.New("not implemented")
