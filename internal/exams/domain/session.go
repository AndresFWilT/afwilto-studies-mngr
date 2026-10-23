package domain

import (
	"fmt"
	"strings"
)

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
func (c SessionConfig) Violations() []Violation {
	var out []Violation
	sel := c.Selection
	switch sel.Kind {
	case SelectSimulation:
		if c.Timing != nil {
			out = append(out, Violation{Field: "config.timing", Rule: RuleLockedBySimulation,
				Message: "a simulation is timed with the exam time; leave timing out"})
		}
		if c.Feedback != "" {
			out = append(out, Violation{Field: "config.feedback", Rule: RuleLockedBySimulation,
				Message: "a simulation shows its answers at the end; leave feedback out"})
		}
		return out
	case SelectRandom:
		out = append(out, countViolations(sel.Count)...)
	case SelectCategories:
		if sel.ClassificationID == "" {
			out = append(out, Violation{Field: "config.selection.classificationId", Rule: "required", Message: "classificationId is required"})
		}
		if len(sel.CategoryIDs) == 0 {
			out = append(out, Violation{Field: "config.selection.categoryIds", Rule: "required", Message: "at least one category id is required"})
		}
		if !sel.All {
			out = append(out, countViolations(sel.Count)...)
		}
	case SelectOriginSet:
		switch set := strings.TrimSpace(sel.Set); {
		case set == "":
			out = append(out, Violation{Field: "config.selection.set", Rule: "required", Message: "set is required"})
		case len([]rune(sel.Set)) > maxSetRunes:
			out = append(out, Violation{Field: "config.selection.set", Rule: "max_length", Message: "set must be at most 120 characters"})
		}
	default:
		return append(out, Violation{Field: "config.selection.kind", Rule: "format",
			Message: "kind must be simulation, random, categories or origin-set"})
	}

	switch {
	case c.Timing == nil:
		out = append(out, Violation{Field: "config.timing", Rule: "required", Message: "timing is required"})
	case c.Timing.Kind == Timed:
		if c.Timing.Minutes < minTimeLimitMin || c.Timing.Minutes > maxTimeLimitMin {
			out = append(out, rangeViolation("config.timing.minutes", minTimeLimitMin, maxTimeLimitMin))
		}
	case c.Timing.Kind != Untimed:
		out = append(out, Violation{Field: "config.timing.kind", Rule: "format", Message: "kind must be untimed or timed"})
	}
	switch c.Feedback {
	case FeedbackEnd, FeedbackEach:
	case "":
		out = append(out, Violation{Field: "config.feedback", Rule: "required", Message: "feedback is required"})
	default:
		out = append(out, Violation{Field: "config.feedback", Rule: "format", Message: "feedback must be end or each"})
	}
	return out
}

const (
	minSessionCount, maxSessionCount = 1, 200
	maxSetRunes                      = 120
)

func countViolations(count int) []Violation {
	if count < minSessionCount || count > maxSessionCount {
		return []Violation{rangeViolation("config.selection.count", minSessionCount, maxSessionCount)}
	}
	return nil
}

// EffectiveFeedback is end for a simulation and the chosen mode otherwise.
func (c SessionConfig) EffectiveFeedback() Feedback {
	if c.Selection.Kind == SelectSimulation {
		return FeedbackEnd
	}
	return c.Feedback
}

// TimedMinutes returns the time limit of a timed session in minutes: the exam
// time for a simulation, the chosen minutes for timed; false when untimed.
func (c SessionConfig) TimedMinutes(exam Exam) (int, bool) {
	if c.Selection.Kind == SelectSimulation {
		return exam.TimeLimitMin, true
	}
	if c.Timing != nil && c.Timing.Kind == Timed {
		return c.Timing.Minutes, true
	}
	return 0, false
}

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

// PreviewSession sizes the pool of a session, the expected draw and its
// suggested time, and lists the shortages the start would fail on. A config
// that breaks a rule, or names a classification or category the exam does not
// have, is a *ValidationError; shortages are a successful read.
func PreviewSession(exam Exam, bank Bank, cfg SessionConfig) (SessionPreview, error) {
	if violations := cfg.Violations(); len(violations) > 0 {
		return SessionPreview{}, NewValidation(violations...)
	}
	pool, err := buildPool(exam, bank, cfg)
	if err != nil {
		return SessionPreview{}, err
	}
	preview := SessionPreview{PoolSize: pool.size(), DrawCount: pool.target, Shortages: pool.shortages}
	switch {
	case preview.DrawCount == 0:
	case cfg.Selection.Kind == SelectSimulation:
		preview.SuggestedMinutes = exam.TimeLimitMin
	default:
		sum := 0.0
		for _, q := range pool.questions {
			sum += QuestionPace(exam, pool.classification, q)
		}
		if pool.target < len(pool.questions) { // the expected draw: N × the mean pace of the pool
			sum = float64(pool.target) * sum / float64(len(pool.questions))
		}
		preview.SuggestedMinutes = CeilMinutes(sum)
	}
	if preview.Shortages == nil {
		preview.Shortages = []Shortage{}
	}
	return preview, nil
}

// shortageViolations turns shortages into the insufficient_questions error.
func shortageViolations(shortages []Shortage) *ValidationError {
	violations := make([]Violation, len(shortages))
	for i := range shortages {
		s := shortages[i]
		violations[i] = Violation{Field: "config", Rule: RuleInsufficientQuestions, Shortage: &s,
			Message: fmt.Sprintf("%s needs %d question(s) and has %d", s.Category, s.Needed, s.Available)}
	}
	return NewValidation(violations...)
}
