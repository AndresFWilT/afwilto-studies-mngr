package domain

import (
	"errors"
	"time"
)

// ExamName is trimmed and 1–120 runes long. Uniqueness among the alive exams
// of a subject compares Key(), the case-folded form (BR-E1).
type ExamName struct{ value string }

func NewExamName(raw string) (ExamName, error) {
	return ExamName{}, errors.New("not implemented")
}

// RehydrateExamName trusts persisted data.
func RehydrateExamName(value string) ExamName { return ExamName{value: value} }

func (n ExamName) String() string { return n.value }
func (n ExamName) Key() string    { return "" }

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

// NewExam validates every field and reports all violations together (BR-E1, BR-E2).
func NewExam(id, subjectID, name string, questionCount, timeLimitMin, passPercent int, now time.Time) (Exam, error) {
	return Exam{}, errors.New("not implemented")
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
	return errors.New("not implemented")
}

// Delete is logical: the row stays, marked deleted (Spec 16 FR-0).
func (e *Exam) Delete(now time.Time) {}
