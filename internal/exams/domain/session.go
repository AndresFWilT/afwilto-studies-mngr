package domain

import "errors"

// SelectionKind is which questions a session takes (Spec 18 FR-7).
type SelectionKind string

const (
	SelectSimulation SelectionKind = "simulation"
	SelectRandom     SelectionKind = "random"
	SelectCategories SelectionKind = "categories"
	SelectOriginSet  SelectionKind = "origin-set"
)

// Selection is the "which questions" choice. Count is the number to take for
// random and categories; All replaces it with "everything" for categories.
type Selection struct {
	Kind             SelectionKind
	Count            int
	All              bool
	ClassificationID string
	CategoryIDs      []string
	Set              string
}

// TimingKind is untimed or timed.
type TimingKind string

const (
	Untimed TimingKind = "untimed"
	Timed   TimingKind = "timed"
)

// Timing is the "how long" choice; Minutes is meaningful when Timed.
type Timing struct {
	Kind    TimingKind
	Minutes int
}

// Feedback is when answers are revealed: at the end (exam mode) or after each
// answer (review mode).
type Feedback string

const (
	FeedbackEnd  Feedback = "end"
	FeedbackEach Feedback = "each"
)

// SessionConfig is what the taker chooses (FR-7). For a simulation Timing is
// nil and Feedback empty: the exam forces them.
type SessionConfig struct {
	Selection Selection
	Timing    *Timing
	Feedback  Feedback
}

// Violations checks the config on its own (no exam needed): a simulation
// carries no timing or feedback (locked_by_simulation), every other kind needs
// both, counts and minutes stay in range. Fields are body paths.
func (c SessionConfig) Violations() []Violation { return nil }

// EffectiveFeedback is end for a simulation and the chosen mode otherwise.
func (c SessionConfig) EffectiveFeedback() Feedback { return "" }

// TimedMinutes returns the time limit of a timed session in minutes: the exam
// time for a simulation, the chosen minutes for timed; false when untimed.
func (c SessionConfig) TimedMinutes(exam Exam) (int, bool) { return 0, false }

// Shortage is a category that has fewer alive questions than a session needs.
type Shortage struct {
	Category  string
	Needed    int
	Available int
}

// SessionPreview is the pure-read answer of POST sessions/preview.
type SessionPreview struct {
	PoolSize         int
	DrawCount        int
	SuggestedMinutes int
	Shortages        []Shortage
}

var errNotImplemented = errors.New("not implemented")

// PreviewSession sizes the pool of a session, the expected draw and its
// suggested time, and lists the shortages the start would fail on.
func PreviewSession(exam Exam, bank Bank, cfg SessionConfig) (SessionPreview, error) {
	return SessionPreview{}, errNotImplemented
}
