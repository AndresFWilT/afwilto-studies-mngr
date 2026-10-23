package domain

import (
	"regexp"
	"slices"
	"strings"
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

// hundredths is 100 × correct / total in hundredths of a percent, rounded half
// up with integers only: no float ever decides a tie. An empty attempt is 0.
func hundredths(correct, total int) int {
	if total <= 0 {
		return 0
	}
	return (20000*correct + total) / (2 * total)
}

// Percent2 is 100 × correct / total rounded half up to two decimals.
func Percent2(correct, total int) float64 { return float64(hundredths(correct, total)) / 100 }

// CorrectLabels are the labels of the snapshot's correct options, in order.
func (s ItemSnapshot) CorrectLabels() []string {
	labels := []string{}
	for _, o := range s.Options {
		if o.Correct {
			labels = append(labels, o.Label)
		}
	}
	return labels
}

// IsCorrect holds when the selected labels are exactly the correct ones: the
// same set, whatever the order (AC-20). A repeated label never matches.
func (s ItemSnapshot) IsCorrect(selected []string) bool {
	want := s.CorrectLabels()
	if len(selected) != len(want) {
		return false
	}
	seen := make(map[string]bool, len(selected))
	for _, label := range selected {
		if seen[label] || !slices.Contains(want, label) {
			return false
		}
		seen[label] = true
	}
	return true
}

// Close scores the attempt as it stands and closes it with the given status at
// the given instant. An unanswered item counts as wrong. passed is set only for
// a simulation, against the pass mark frozen at the start. The receiver is not
// changed.
func (a Attempt) Close(status AttemptStatus, at time.Time) Attempt {
	closed := a
	closed.Items = slices.Clone(a.Items)
	correct := 0
	for i := range closed.Items {
		ok := closed.Items[i].Snapshot.IsCorrect(closed.Items[i].Selected)
		closed.Items[i].Correct = &ok
		if ok {
			correct++
		}
	}
	h := hundredths(correct, len(closed.Items))
	score := float64(h) / 100
	closed.Status, closed.SubmittedAt, closed.ScorePercent, closed.Passed = status, &at, &score, nil
	if a.Config.Selection.Kind == SelectSimulation {
		passed := h >= a.PassPercent*100
		closed.Passed = &passed
	}
	return closed
}

// Submit closes the attempt as submitted. A timed attempt submitted inside the
// grace period past its deadline is closed at the deadline, so its duration
// never exceeds the time limit.
func (a Attempt) Submit(now time.Time) Attempt {
	if a.DeadlineAt != nil && now.After(*a.DeadlineAt) {
		now = *a.DeadlineAt
	}
	return a.Close(AttemptSubmitted, now)
}

// Expire closes the attempt as expired at its deadline, however late the
// request that found it: the duration is the time the taker had.
func (a Attempt) Expire() Attempt {
	at := a.StartedAt
	if a.DeadlineAt != nil {
		at = *a.DeadlineAt
	}
	return a.Close(AttemptExpired, at)
}

// Result is the score of a closed attempt.
func (a Attempt) Result() AttemptResult {
	correct := 0
	for _, it := range a.Items {
		if it.Correct != nil && *it.Correct {
			correct++
		}
	}
	r := AttemptResult{
		ScorePercent: Percent2(correct, len(a.Items)), Passed: a.Passed, CorrectCount: correct, Total: len(a.Items),
		Breakdown: breakdown(a.Items),
	}
	if a.SubmittedAt != nil {
		r.DurationSeconds = int(a.SubmittedAt.Sub(a.StartedAt) / time.Second)
	}
	return r
}

// breakdown tallies each item under every category it carries, per
// classification: classifications by name, categories weakest first (percent
// ascending, then name). An item with no category in a classification is not
// part of it.
func breakdown(items []AttemptItem) []ClassificationScore {
	type tally struct{ correct, total int }
	byClass := map[string]map[string]*tally{}
	for _, it := range items {
		for _, ref := range it.Snapshot.Categories {
			if byClass[ref.Classification] == nil {
				byClass[ref.Classification] = map[string]*tally{}
			}
			t := byClass[ref.Classification][ref.Category]
			if t == nil {
				t = &tally{}
				byClass[ref.Classification][ref.Category] = t
			}
			t.total++
			if it.Correct != nil && *it.Correct {
				t.correct++
			}
		}
	}
	out := make([]ClassificationScore, 0, len(byClass))
	for classification, cats := range byClass {
		scores := make([]CategoryScore, 0, len(cats))
		for name, t := range cats {
			scores = append(scores, CategoryScore{Name: name, Correct: t.correct, Total: t.total, Percent: Percent2(t.correct, t.total)})
		}
		slices.SortFunc(scores, func(x, y CategoryScore) int {
			switch {
			case x.Percent < y.Percent:
				return -1
			case x.Percent > y.Percent:
				return 1
			}
			return compareNames(x.Name, y.Name)
		})
		out = append(out, ClassificationScore{Classification: classification, Categories: scores})
	}
	slices.SortFunc(out, func(x, y ClassificationScore) int { return compareNames(x.Classification, y.Classification) })
	return out
}

// compareNames orders names case-insensitively, then bytewise, so the order is
// total and does not depend on map iteration.
func compareNames(a, b string) int {
	if c := strings.Compare(strings.ToLower(a), strings.ToLower(b)); c != 0 {
		return c
	}
	return strings.Compare(a, b)
}

var slideRef = regexp.MustCompile(`\s*\(diap\.[^)]*\)\s*$`)

// NoteTitleFromRef is the note title an origin.noteRef points at: the ref
// without a trailing "(diap. …)" slide reference, trimmed.
func NoteTitleFromRef(ref string) string {
	return strings.TrimSpace(slideRef.ReplaceAllString(ref, ""))
}
