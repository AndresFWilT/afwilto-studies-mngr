package domain

import (
	"fmt"
	"slices"
	"time"
)

// DeadlineGrace is how long after deadlineAt a write still counts (BR-A2).
const DeadlineGrace = 5 * time.Second

// PastDeadline reports whether the attempt is open, timed and beyond its
// deadline plus the grace period: now > deadlineAt + 5 s.
func (a Attempt) PastDeadline(now time.Time) bool {
	return a.Status == AttemptInProgress && a.DeadlineAt != nil && now.After(a.DeadlineAt.Add(DeadlineGrace))
}

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
// check of the selection while the item has none yet. Options without a
// rationale are left out of Rationales.
func (it AttemptItem) Reveal() Reveal {
	correct := it.Snapshot.IsCorrect(it.Selected)
	if it.Correct != nil {
		correct = *it.Correct
	}
	rationales := map[string]string{}
	for _, o := range it.Snapshot.Options {
		if o.Rationale != "" {
			rationales[o.Label] = o.Rationale
		}
	}
	return Reveal{Correct: correct, CorrectLabels: it.Snapshot.CorrectLabels(), Rationales: rationales, Explanation: it.Snapshot.Explanation}
}

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
// not changed. The checks run in this order: closed, unknown position, labels,
// count, lock. The caller handles the deadline before it gets here.
func (a Attempt) Answer(position int, in AnswerInput, now time.Time) (Attempt, AnswerOutcome, error) {
	if a.Status != AttemptInProgress {
		return a, AnswerOutcome{}, NewAttemptClosed()
	}
	if position < 0 || position >= len(a.Items) {
		return a, AnswerOutcome{}, ErrNotFound
	}
	item := a.Items[position]
	if violations := labelViolations(item.Snapshot, in.Selected); len(violations) > 0 {
		return a, AnswerOutcome{}, NewValidation(violations...)
	}
	limit := 1
	if item.Snapshot.Type == QuestionMulti {
		limit = item.Snapshot.ChooseCount()
	}
	if len(in.Selected) > limit {
		return a, AnswerOutcome{}, SingleViolation("selected", RuleTooManySelected,
			fmt.Sprintf("at most %d option(s) can be selected, got %d", limit, len(in.Selected)))
	}
	if item.Revealed && !sameLabels(item.Selected, in.Selected) {
		return a, AnswerOutcome{}, NewAnswerLocked()
	}

	if !item.Revealed {
		item.Selected = slices.Clone(in.Selected)
		if item.Selected == nil {
			item.Selected = []string{}
		}
		switch {
		case len(item.Selected) == 0:
			item.AnsweredAt = nil
		case item.AnsweredAt == nil || !sameLabels(a.Items[position].Selected, item.Selected):
			at := now
			item.AnsweredAt = &at
		}
		if a.Config.EffectiveFeedback() == FeedbackEach && len(item.Selected) > 0 {
			correct := item.Snapshot.IsCorrect(item.Selected)
			item.Revealed, item.Correct = true, &correct
		}
	}
	item.Flagged = in.Flagged

	next := a
	next.Items = slices.Clone(a.Items)
	next.Items[position] = item
	out := AnswerOutcome{Position: position, Selected: item.Selected, Flagged: item.Flagged}
	if item.Revealed {
		reveal := item.Reveal()
		out.Reveal = &reveal
	}
	return next, out, nil
}

// labelViolations checks that every label is one the item shows and that none
// repeats; a repeat is reported on its second occurrence.
func labelViolations(s ItemSnapshot, selected []string) []Violation {
	var out []Violation
	seen := map[string]bool{}
	for i, label := range selected {
		field := fmt.Sprintf("selected[%d]", i)
		switch {
		case !slices.ContainsFunc(s.Options, func(o SnapshotOption) bool { return o.Label == label }):
			out = append(out, Violation{Field: field, Rule: RuleInvalidLabel, Message: fmt.Sprintf("%q is not an option of this question", label)})
		case seen[label]:
			out = append(out, Violation{Field: field, Rule: RuleInvalidLabel, Message: fmt.Sprintf("label %s is repeated", label)})
		}
		seen[label] = true
	}
	return out
}

// sameLabels reports whether two selections hold the same labels, whatever the
// order.
func sameLabels(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, label := range a {
		if !slices.Contains(b, label) {
			return false
		}
	}
	return true
}
