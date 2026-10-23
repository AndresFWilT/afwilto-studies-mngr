package domain

import (
	"strings"
	"testing"
)

func opts(correct ...bool) []Option {
	out := make([]Option, len(correct))
	for i, c := range correct {
		out[i] = Option{Text: "option " + string(rune('A'+i)), Correct: c}
	}
	return out
}

func violationSet(vs []Violation) map[string]bool {
	set := map[string]bool{}
	for _, v := range vs {
		set[v.Field+"|"+v.Rule] = true
	}
	return set
}

func TestValidateQuestionAcceptsWellFormedQuestions(t *testing.T) { // BR-Q1/Q2 happy paths
	cases := map[string]FileQuestion{
		"single, 3 options, 1 correct":  {Key: "q1", Type: QuestionSingle, Stem: "s", Options: opts(false, true, false)},
		"single, 2 options":             {Key: "q1", Type: QuestionSingle, Stem: "s", Options: opts(true, false)},
		"single, 6 options":             {Key: "q1", Type: QuestionSingle, Stem: "s", Options: opts(false, false, false, false, false, true)},
		"multi, 3 options, 2 correct":   {Key: "q1", Type: QuestionMulti, Stem: "s", Options: opts(true, true, false)},
		"multi, 6 options, 5 correct":   {Key: "q1", Type: QuestionMulti, Stem: "s", Options: opts(true, true, true, true, true, false)},
		"image option with alt":         {Key: "q1", Type: QuestionSingle, Stem: "s", Options: []Option{{Image: "media:a.png", Alt: "a", Correct: true}, {Text: "b"}}},
		"image and text together":       {Key: "q1", Type: QuestionSingle, Stem: "s", Options: []Option{{Text: "a", Image: "media:a.png", Alt: "alt", Correct: true}, {Text: "b"}}},
		"key with dots, dashes, digits": {Key: "td-saa.01_x-9", Type: QuestionSingle, Stem: "s", Options: opts(true, false)},
		"key of exactly 100 characters": {Key: strings.Repeat("a", 100), Type: QuestionSingle, Stem: "s", Options: opts(true, false)},
	}
	for name, q := range cases {
		t.Run(name, func(t *testing.T) {
			if got := ValidateQuestion(q); len(got) != 0 {
				t.Errorf("violations = %+v, want none", got)
			}
		})
	}
}

func TestValidateQuestionReportsEachBrokenRuleAtItsPath(t *testing.T) { // BR-Q1, BR-Q2, BR-Q3 pattern
	cases := []struct {
		name  string
		q     FileQuestion
		field string
		rule  string
	}{
		{"single with two correct", FileQuestion{Key: "q", Type: QuestionSingle, Options: opts(true, true, false)}, "options", RuleCorrectCount},
		{"single with no correct", FileQuestion{Key: "q", Type: QuestionSingle, Options: opts(false, false)}, "options", RuleCorrectCount},
		{"multi with one correct", FileQuestion{Key: "q", Type: QuestionMulti, Options: opts(true, false, false)}, "options", RuleCorrectCount},
		{"multi with every option correct", FileQuestion{Key: "q", Type: QuestionMulti, Options: opts(true, true, true)}, "options", RuleCorrectCount},
		{"multi with two options", FileQuestion{Key: "q", Type: QuestionMulti, Options: opts(true, false)}, "options", RuleCorrectCount},
		{"option with neither text nor image", FileQuestion{Key: "q", Type: QuestionSingle, Options: []Option{{Correct: true}, {Text: "b"}}}, "options[0]", RuleOptionContent},
		{"option with only blank text", FileQuestion{Key: "q", Type: QuestionSingle, Options: []Option{{Text: "  \n", Correct: true}, {Text: "b"}}}, "options[0]", RuleOptionContent},
		{"image without alt", FileQuestion{Key: "q", Type: QuestionSingle, Options: []Option{{Text: "a", Correct: true}, {Image: "media:x.png"}}}, "options[1]", RuleOptionContent},
		{"key with uppercase", FileQuestion{Key: "Q1", Type: QuestionSingle, Options: opts(true, false)}, "key", RulePattern},
		{"key with a space", FileQuestion{Key: "q 1", Type: QuestionSingle, Options: opts(true, false)}, "key", RulePattern},
		{"empty key", FileQuestion{Key: "", Type: QuestionSingle, Options: opts(true, false)}, "key", RulePattern},
		{"key starting with a dash", FileQuestion{Key: "-q", Type: QuestionSingle, Options: opts(true, false)}, "key", RulePattern},
		{"key of 101 characters", FileQuestion{Key: strings.Repeat("a", 101), Type: QuestionSingle, Options: opts(true, false)}, "key", RulePattern},
		{"unknown type", FileQuestion{Key: "q", Type: "bool", Options: opts(true, false)}, "type", RuleSchema},
		{"one option", FileQuestion{Key: "q", Type: QuestionSingle, Options: opts(true)}, "options", RuleSchema},
		{"seven options", FileQuestion{Key: "q", Type: QuestionSingle, Options: opts(true, false, false, false, false, false, false)}, "options", RuleSchema},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateQuestion(tc.q)

			if !violationSet(got)[tc.field+"|"+tc.rule] {
				t.Errorf("violations = %+v, want %s at %s", got, tc.rule, tc.field)
			}
			for _, v := range got {
				if v.Message == "" {
					t.Errorf("violation %+v has no message", v)
				}
			}
		})
	}
}

func TestValidateQuestionCollectsEveryOptionViolation(t *testing.T) { // each stage collects all its errors
	q := FileQuestion{Key: "q", Type: QuestionSingle, Options: []Option{{Correct: true}, {Image: "media:x"}, {Text: "ok"}}}

	got := violationSet(ValidateQuestion(q))

	if !got["options[0]|option_content"] || !got["options[1]|option_content"] || got["options[2]|option_content"] {
		t.Errorf("violations = %v, want option_content on options[0] and options[1] only", got)
	}
}

func TestValidateQuestionReportsCorrectCountOnceNotPerCheck(t *testing.T) {
	q := FileQuestion{Key: "q", Type: QuestionMulti, Options: opts(true, true)} // too few options AND no wrong option

	n := 0
	for _, v := range ValidateQuestion(q) {
		if v.Rule == RuleCorrectCount {
			n++
		}
	}
	if n != 1 {
		t.Errorf("correct_count violations = %d, want 1", n)
	}
}

func TestValidateQuestionCapsOriginNumberFromOneToOneMillion(t *testing.T) { // phase-2 fix 1
	base := FileQuestion{Key: "q", Type: QuestionSingle, Options: opts(true, false)}
	for name, tc := range map[string]struct {
		origin *Origin
		ok     bool
	}{
		"no origin":            {nil, true},
		"number absent (zero)": {&Origin{Set: "S"}, true},
		"number 1":             {&Origin{Set: "S", Number: 1}, true},
		"number 1,000,000":     {&Origin{Set: "S", Number: 1_000_000}, true},
		"number 1,000,001":     {&Origin{Set: "S", Number: 1_000_001}, false},
		"number 3,000,000,000": {&Origin{Set: "S", Number: 3_000_000_000}, false},
		"negative number":      {&Origin{Set: "S", Number: -1}, false},
	} {
		q := base
		q.Origin = tc.origin

		got := violationSet(ValidateQuestion(q))

		if has := got["origin.number|"+RuleRange]; has == tc.ok {
			t.Errorf("%s: violations %v, want range at origin.number: %v", name, got, !tc.ok)
		}
	}
}
