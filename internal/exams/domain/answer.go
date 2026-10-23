package domain

import (
	"errors"
	"time"
)

var errNotImplemented = errors.New("not implemented")

// DeadlineGrace is how long after deadlineAt a write still counts (BR-A2).
const DeadlineGrace = 5 * time.Second

// PastDeadline reports whether the attempt is open, timed and beyond its
// deadline plus the grace period: now > deadlineAt + 5 s.
func (a Attempt) PastDeadline(now time.Time) bool { return false }

// AnswerInput is the body of a PUT: the labels chosen and the review flag.
type AnswerInput struct {
	Selected []string
	Flagged  bool
}

// Reveal is what feedback "each" and the review show about an item: the
// correctness, the answer key, the rationales by label and the explanation.
type Reveal struct {
	Correct       bool
	CorrectLabels []string
	Rationales    map[string]string
	Explanation   string
}

// Reveal is the reveal of an item; Correct is the stored value, or the exact-set
// check of the selection while the item has none yet.
func (it AttemptItem) Reveal() Reveal { return Reveal{} }

// AnswerOutcome is the result of saving an answer. Reveal is set only when the
// item is revealed (feedback each, non-empty selection).
type AnswerOutcome struct {
	Position int
	Selected []string
	Flagged  bool
	Reveal   *Reveal
}

// Answer saves the selection and flag of one item of an open attempt and
// returns the changed attempt and what the response may show. The receiver is
// not changed.
func (a Attempt) Answer(position int, in AnswerInput, now time.Time) (Attempt, AnswerOutcome, error) {
	return a, AnswerOutcome{}, errNotImplemented
}
