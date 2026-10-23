package domain

import (
	"slices"
	"testing"
)

func violationKey(v Violation) string { return v.Field + ":" + v.Rule }

func keysOf(vs []Violation) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = violationKey(v)
	}
	return out
}

func TestSimulationForbidsTimingAndFeedback(t *testing.T) { // S06a-5, FR-7
	timed := &Timing{Kind: Timed, Minutes: 30}
	for name, tc := range map[string]struct {
		cfg  SessionConfig
		want []string
	}{
		"timing sent":            {SessionConfig{Selection: Selection{Kind: SelectSimulation}, Timing: timed}, []string{"config.timing:locked_by_simulation"}},
		"feedback sent":          {SessionConfig{Selection: Selection{Kind: SelectSimulation}, Feedback: FeedbackEach}, []string{"config.feedback:locked_by_simulation"}},
		"both sent":              {SessionConfig{Selection: Selection{Kind: SelectSimulation}, Timing: timed, Feedback: FeedbackEnd}, []string{"config.timing:locked_by_simulation", "config.feedback:locked_by_simulation"}},
		"untimed is also timing": {SessionConfig{Selection: Selection{Kind: SelectSimulation}, Timing: &Timing{Kind: Untimed}}, []string{"config.timing:locked_by_simulation"}},
		"neither: fine":          {simulation(), nil},
	} {
		t.Run(name, func(t *testing.T) {
			if got := keysOf(tc.cfg.Violations()); !slices.Equal(got, tc.want) {
				t.Errorf("violations = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOtherSelectionsRequireTimingAndFeedback(t *testing.T) { // FR-7
	random := Selection{Kind: SelectRandom, Count: 10}
	for name, tc := range map[string]struct {
		cfg  SessionConfig
		want []string
	}{
		"neither":             {SessionConfig{Selection: random}, []string{"config.timing:required", "config.feedback:required"}},
		"no feedback":         {SessionConfig{Selection: random, Timing: &Timing{Kind: Untimed}}, []string{"config.feedback:required"}},
		"no timing":           {SessionConfig{Selection: random, Feedback: FeedbackEach}, []string{"config.timing:required"}},
		"both":                {practice(random), nil},
		"unknown feedback":    {SessionConfig{Selection: random, Timing: &Timing{Kind: Untimed}, Feedback: "later"}, []string{"config.feedback:format"}},
		"timed 0 minutes":     {SessionConfig{Selection: random, Timing: &Timing{Kind: Timed, Minutes: 0}, Feedback: FeedbackEnd}, []string{"config.timing.minutes:range"}},
		"timed 601 minutes":   {SessionConfig{Selection: random, Timing: &Timing{Kind: Timed, Minutes: 601}, Feedback: FeedbackEnd}, []string{"config.timing.minutes:range"}},
		"timed 600 is fine":   {SessionConfig{Selection: random, Timing: &Timing{Kind: Timed, Minutes: 600}, Feedback: FeedbackEnd}, nil},
		"timed 1 is fine":     {SessionConfig{Selection: random, Timing: &Timing{Kind: Timed, Minutes: 1}, Feedback: FeedbackEnd}, nil},
		"unknown timing kind": {SessionConfig{Selection: random, Timing: &Timing{Kind: "whenever"}, Feedback: FeedbackEnd}, []string{"config.timing.kind:format"}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := keysOf(tc.cfg.Violations()); !slices.Equal(got, tc.want) {
				t.Errorf("violations = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSelectionCountsAndFieldsAreChecked(t *testing.T) { // slice 06a §1.1
	ok := func(sel Selection) []string { return keysOf(practice(sel).Violations()) }

	for name, tc := range map[string]struct {
		sel  Selection
		want []string
	}{
		"random 0":                     {Selection{Kind: SelectRandom, Count: 0}, []string{"config.selection.count:range"}},
		"random 201":                   {Selection{Kind: SelectRandom, Count: 201}, []string{"config.selection.count:range"}},
		"random 200":                   {Selection{Kind: SelectRandom, Count: 200}, nil},
		"random 1":                     {Selection{Kind: SelectRandom, Count: 1}, nil},
		"categories 0":                 {Selection{Kind: SelectCategories, ClassificationID: "c", CategoryIDs: []string{"k"}}, []string{"config.selection.count:range"}},
		"categories all":               {Selection{Kind: SelectCategories, ClassificationID: "c", CategoryIDs: []string{"k"}, All: true}, nil},
		"categories no ids":            {Selection{Kind: SelectCategories, ClassificationID: "c", All: true}, []string{"config.selection.categoryIds:required"}},
		"categories no classification": {Selection{Kind: SelectCategories, CategoryIDs: []string{"k"}, All: true}, []string{"config.selection.classificationId:required"}},
		"origin-set blank":             {Selection{Kind: SelectOriginSet, Set: "  "}, []string{"config.selection.set:required"}},
		"origin-set too long":          {Selection{Kind: SelectOriginSet, Set: string(make([]byte, 121))}, []string{"config.selection.set:max_length"}},
		"unknown kind":                 {Selection{Kind: "everything"}, []string{"config.selection.kind:format"}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ok(tc.sel); !slices.Equal(got, tc.want) {
				t.Errorf("violations = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestEffectiveFeedbackAndTime(t *testing.T) { // FR-7: a simulation is timed with the exam time and shows answers at the end
	exam := sessExam()

	if got := simulation().EffectiveFeedback(); got != FeedbackEnd {
		t.Errorf("simulation feedback = %q, want end", got)
	}
	if m, timed := simulation().TimedMinutes(exam); !timed || m != 130 {
		t.Errorf("simulation time = %d/%v, want 130 min timed", m, timed)
	}
	each := SessionConfig{Selection: Selection{Kind: SelectRandom, Count: 5}, Timing: &Timing{Kind: Timed, Minutes: 30}, Feedback: FeedbackEach}
	if each.EffectiveFeedback() != FeedbackEach {
		t.Error("a chosen feedback must be kept")
	}
	if m, timed := each.TimedMinutes(exam); !timed || m != 30 {
		t.Errorf("timed time = %d/%v, want 30", m, timed)
	}
	if _, timed := practice(Selection{Kind: SelectRandom, Count: 5}).TimedMinutes(exam); timed {
		t.Error("an untimed session reported a time limit")
	}
}
