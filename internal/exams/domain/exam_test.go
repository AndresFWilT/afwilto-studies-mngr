package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

var examNow = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

func ptr[T any](v T) *T { return &v }

// violationsOf returns the violations of a *ValidationError, failing the test
// when err is anything else.
func violationsOf(t *testing.T, err error) []Violation {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v, want a *ValidationError", err)
	}
	return v.Violations
}

func wantViolations(t *testing.T, err error, want ...Violation) {
	t.Helper()
	got := violationsOf(t, err)
	if len(got) != len(want) {
		t.Fatalf("violations = %+v, want fields/rules %+v", got, want)
	}
	for i := range want {
		if got[i].Field != want[i].Field || got[i].Rule != want[i].Rule {
			t.Errorf("violation[%d] = %s/%s, want %s/%s", i, got[i].Field, got[i].Rule, want[i].Field, want[i].Rule)
		}
		if got[i].Message == "" {
			t.Errorf("violation[%d] has no message", i)
		}
	}
}

func TestNewExamName(t *testing.T) { // BR-E1 normalization
	tests := []struct {
		name, raw, want, rule string
	}{
		{"trims", "  SAA-C03 ", "SAA-C03", ""},
		{"120 runes", strings.Repeat("ñ", 120), strings.Repeat("ñ", 120), ""},
		{"121 runes", strings.Repeat("ñ", 121), "", "max_length"},
		{"empty", "", "", "required"},
		{"blank", " \t ", "", "required"},
		{"control char", "SAA\x00C03", "", "format"},
		{"newline inside", "SAA\nC03", "", "format"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewExamName(tc.raw)
			if tc.rule == "" {
				if err != nil || got.String() != tc.want {
					t.Fatalf("got (%q, %v), want %q", got.String(), err, tc.want)
				}
				return
			}
			wantViolations(t, err, Violation{Field: "name", Rule: tc.rule})
		})
	}
}

func TestExamNameKeyIgnoresCaseAndSurroundingSpaces(t *testing.T) { // BR-E1, S02-2
	a, _ := NewExamName("SAA-C03")
	b, _ := NewExamName("  saa-c03 ")
	c, _ := NewExamName("SAA-C04")
	if a.Key() != b.Key() {
		t.Errorf("keys differ: %q vs %q", a.Key(), b.Key())
	}
	if a.Key() == c.Key() {
		t.Errorf("different names share the key %q", a.Key())
	}
}

func TestNewExamBuildsTheAggregateAtVersionOne(t *testing.T) { // S02-1
	e, err := NewExam("id-1", "subject-1", " SAA-C03 ", 65, 130, 72, examNow)
	if err != nil {
		t.Fatal(err)
	}
	if e.ID != "id-1" || e.SubjectID != "subject-1" || e.Name.String() != "SAA-C03" ||
		e.QuestionCount != 65 || e.TimeLimitMin != 130 || e.PassPercent != 72 {
		t.Errorf("exam = %+v", e)
	}
	if e.Version != 1 || !e.CreatedAt.Equal(examNow) || !e.UpdatedAt.Equal(examNow) || e.DeletedAt != nil {
		t.Errorf("version/timestamps = %d %v %v %v", e.Version, e.CreatedAt, e.UpdatedAt, e.DeletedAt)
	}
}

func TestNewExamRangeBoundaries(t *testing.T) { // BR-E2 boundaries
	tests := []struct {
		name                       string
		questionCount, limit, pass int
		wantFields                 []string
	}{
		{"lower bounds", 1, 1, 1, nil},
		{"upper bounds", 200, 600, 100, nil},
		{"questionCount 0", 0, 130, 72, []string{"questionCount"}},
		{"questionCount 201", 201, 130, 72, []string{"questionCount"}},
		{"timeLimitMin 0", 65, 0, 72, []string{"timeLimitMin"}},
		{"timeLimitMin 601", 65, 601, 72, []string{"timeLimitMin"}},
		{"passPercent 0", 65, 130, 0, []string{"passPercent"}},
		{"passPercent 101", 65, 130, 101, []string{"passPercent"}},
		{"negative values", -1, -5, -3, []string{"questionCount", "timeLimitMin", "passPercent"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewExam("id", "s", "N", tc.questionCount, tc.limit, tc.pass, examNow)
			if tc.wantFields == nil {
				if err != nil {
					t.Fatalf("err = %v, want none", err)
				}
				return
			}
			var want []Violation
			for _, f := range tc.wantFields {
				want = append(want, Violation{Field: f, Rule: RuleRange})
			}
			wantViolations(t, err, want...)
		})
	}
}

func TestNewExamReportsEveryViolationAtOnce(t *testing.T) { // S02-3, name first then numbers in field order
	_, err := NewExam("id", "s", " ", 0, 601, 101, examNow)

	wantViolations(t, err,
		Violation{Field: "name", Rule: "required"},
		Violation{Field: "questionCount", Rule: RuleRange},
		Violation{Field: "timeLimitMin", Rule: RuleRange},
		Violation{Field: "passPercent", Rule: RuleRange},
	)
}

func newTestExam(t *testing.T) Exam {
	t.Helper()
	e, err := NewExam("id-1", "subject-1", "SAA-C03", 65, 130, 72, examNow)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestEditChangesOnlyTheSetFieldsAndBumpsTheVersion(t *testing.T) { // S02-5
	e := newTestExam(t)
	later := examNow.Add(time.Hour)

	if err := e.Edit(ExamEdit{Name: ptr(" SAA-C04 "), PassPercent: ptr(75)}, later); err != nil {
		t.Fatal(err)
	}

	if e.Name.String() != "SAA-C04" || e.PassPercent != 75 || e.QuestionCount != 65 || e.TimeLimitMin != 130 {
		t.Errorf("exam = %+v", e)
	}
	if e.Version != 2 || !e.UpdatedAt.Equal(later) || !e.CreatedAt.Equal(examNow) {
		t.Errorf("version/timestamps = %d %v %v", e.Version, e.UpdatedAt, e.CreatedAt)
	}
}

func TestEditIsAtomicWhenAnyFieldIsInvalid(t *testing.T) { // BR-E2 on edit
	e := newTestExam(t)

	err := e.Edit(ExamEdit{Name: ptr("Valid"), QuestionCount: ptr(0), TimeLimitMin: ptr(601)}, examNow.Add(time.Hour))

	wantViolations(t, err,
		Violation{Field: "questionCount", Rule: RuleRange},
		Violation{Field: "timeLimitMin", Rule: RuleRange},
	)
	if e.Name.String() != "SAA-C03" || e.QuestionCount != 65 || e.Version != 1 || !e.UpdatedAt.Equal(examNow) {
		t.Errorf("a rejected edit must leave the exam untouched, got %+v", e)
	}
}

func TestEditRejectsAnInvalidName(t *testing.T) {
	e := newTestExam(t)

	wantViolations(t, e.Edit(ExamEdit{Name: ptr("  ")}, examNow), Violation{Field: "name", Rule: "required"})
}

func TestDeleteIsLogicalAndKeepsTheVersion(t *testing.T) { // S02-6
	e := newTestExam(t)
	later := examNow.Add(time.Hour)

	e.Delete(later)

	if e.DeletedAt == nil || !e.DeletedAt.Equal(later) || !e.UpdatedAt.Equal(later) || e.Version != 1 {
		t.Errorf("exam = %+v", e)
	}
}
