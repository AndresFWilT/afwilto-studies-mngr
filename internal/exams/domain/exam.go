package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

const (
	maxExamNameRunes = 120

	minQuestionCount, maxQuestionCount = 1, 200
	minTimeLimitMin, maxTimeLimitMin   = 1, 600
	minPassPercent, maxPassPercent     = 1, 100
)

// ExamName is trimmed and 1–120 runes long. Uniqueness among the alive exams
// of a subject compares Key(), the case-folded form (BR-E1).
type ExamName struct{ value string }

func NewExamName(raw string) (ExamName, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ExamName{}, SingleViolation("name", "required", "name must not be empty")
	}
	if strings.ContainsFunc(trimmed, unicode.IsControl) {
		return ExamName{}, SingleViolation("name", "format", "name must not contain control characters")
	}
	if len([]rune(trimmed)) > maxExamNameRunes {
		return ExamName{}, SingleViolation("name", "max_length", "name must be at most 120 characters")
	}
	return ExamName{value: trimmed}, nil
}

// RehydrateExamName trusts persisted data.
func RehydrateExamName(value string) ExamName { return ExamName{value: value} }

func (n ExamName) String() string { return n.value }
func (n ExamName) Key() string    { return strings.ToLower(n.value) }

// Exam is a simulation template that hangs off a subject (Spec 18 §2).
type Exam struct {
	ID            string
	SubjectID     string
	Name          ExamName
	QuestionCount int
	TimeLimitMin  int
	PassPercent   int
	Version       int
	CreatedAt     time.Time
	UpdatedAt     time.Time
	DeletedAt     *time.Time
}

// rangeViolation is the BR-E2 violation of one numeric field.
func rangeViolation(field string, min, max int) Violation {
	return Violation{Field: field, Rule: RuleRange, Message: fmt.Sprintf("%s must be between %d and %d", field, min, max)}
}

// checkRanges reports a violation per out-of-range number, in field order.
func checkRanges(violations []Violation, questionCount, timeLimitMin, passPercent *int) []Violation {
	if questionCount != nil && (*questionCount < minQuestionCount || *questionCount > maxQuestionCount) {
		violations = append(violations, rangeViolation("questionCount", minQuestionCount, maxQuestionCount))
	}
	if timeLimitMin != nil && (*timeLimitMin < minTimeLimitMin || *timeLimitMin > maxTimeLimitMin) {
		violations = append(violations, rangeViolation("timeLimitMin", minTimeLimitMin, maxTimeLimitMin))
	}
	if passPercent != nil && (*passPercent < minPassPercent || *passPercent > maxPassPercent) {
		violations = append(violations, rangeViolation("passPercent", minPassPercent, maxPassPercent))
	}
	return violations
}

// nameViolations validates a raw name and appends its violations.
func nameViolations(violations []Violation, raw string) (ExamName, []Violation) {
	name, err := NewExamName(raw)
	if err != nil {
		var v *ValidationError
		if errors.As(err, &v) {
			violations = append(violations, v.Violations...)
		}
	}
	return name, violations
}

// NewExam validates every field and reports all violations together (BR-E1, BR-E2).
func NewExam(id, subjectID, name string, questionCount, timeLimitMin, passPercent int, now time.Time) (Exam, error) {
	validName, violations := nameViolations(nil, name)
	violations = checkRanges(violations, &questionCount, &timeLimitMin, &passPercent)
	if len(violations) > 0 {
		return Exam{}, NewValidation(violations...)
	}
	return Exam{
		ID: id, SubjectID: subjectID, Name: validName,
		QuestionCount: questionCount, TimeLimitMin: timeLimitMin, PassPercent: passPercent,
		Version: 1, CreatedAt: now, UpdatedAt: now,
	}, nil
}

// ExamEdit carries the editable fields of PATCH; nil means "leave unchanged".
type ExamEdit struct {
	Name          *string
	QuestionCount *int
	TimeLimitMin  *int
	PassPercent   *int
}

// Edit applies every set field or none, and increments Version on success.
func (e *Exam) Edit(edit ExamEdit, now time.Time) error {
	name := e.Name
	var violations []Violation
	if edit.Name != nil {
		name, violations = nameViolations(violations, *edit.Name)
	}
	violations = checkRanges(violations, edit.QuestionCount, edit.TimeLimitMin, edit.PassPercent)
	if len(violations) > 0 {
		return NewValidation(violations...)
	}

	e.Name = name
	if edit.QuestionCount != nil {
		e.QuestionCount = *edit.QuestionCount
	}
	if edit.TimeLimitMin != nil {
		e.TimeLimitMin = *edit.TimeLimitMin
	}
	if edit.PassPercent != nil {
		e.PassPercent = *edit.PassPercent
	}
	e.Version++
	e.UpdatedAt = now
	return nil
}

// Delete is logical: the row stays, marked deleted (Spec 16 FR-0).
func (e *Exam) Delete(now time.Time) {
	e.DeletedAt = &now
	e.UpdatedAt = now
}

// SimulationResult is the outcome of the latest closed simulation attempt;
// slice 06b fills it in.
type SimulationResult struct {
	ScorePercent float64
	Passed       bool
	At           time.Time
}

// ExamSummary is the read model: the exam plus what its questions contribute.
// QuestionTotal counts the alive questions, LastSimulation is nil until slice
// 06b. Classifications (with their categories) fill the detail view; list
// items leave them nil.
type ExamSummary struct {
	Exam
	QuestionTotal   int
	LastSimulation  *SimulationResult
	Classifications []Classification
}
