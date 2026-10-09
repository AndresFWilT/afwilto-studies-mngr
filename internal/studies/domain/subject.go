package domain

import (
	"strings"
	"time"
	"unicode"
)

const (
	maxSubjectNameRunes        = 120
	maxSubjectDescriptionRunes = 2000
)

// SubjectName is trimmed and 1–120 runes long. Uniqueness among the alive
// subjects of a category compares Key(), the case-folded form.
type SubjectName struct{ value string }

func NewSubjectName(raw string) (SubjectName, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return SubjectName{}, SingleViolation("name", "required", "name must not be empty")
	}
	if strings.ContainsFunc(trimmed, unicode.IsControl) {
		return SubjectName{}, SingleViolation("name", "format", "name must not contain control characters")
	}
	if len([]rune(trimmed)) > maxSubjectNameRunes {
		return SubjectName{}, SingleViolation("name", "max_length", "name must be at most 120 characters")
	}
	return SubjectName{value: trimmed}, nil
}

// RehydrateSubjectName trusts persisted data.
func RehydrateSubjectName(value string) SubjectName { return SubjectName{value: value} }

func (n SubjectName) String() string { return n.value }
func (n SubjectName) Key() string    { return strings.ToLower(n.value) }

// NewSubjectDescription validates an optional multi-line description: nil and
// the empty string both mean "no description"; only NUL is rejected because
// Postgres text cannot store it.
func NewSubjectDescription(raw *string) (*string, error) {
	if raw == nil || *raw == "" {
		return nil, nil
	}
	if strings.ContainsRune(*raw, 0) {
		return nil, SingleViolation("description", "format", "description must not contain NUL characters")
	}
	if len([]rune(*raw)) > maxSubjectDescriptionRunes {
		return nil, SingleViolation("description", "max_length", "description must be at most 2000 characters")
	}
	return raw, nil
}

// Subject is a study topic that belongs to a category (any depth) and holds
// notes.
type Subject struct {
	ID          string
	CategoryID  string
	Name        SubjectName
	Description *string
	CreatedAt   time.Time
	UpdatedAt   time.Time
	DeletedAt   *time.Time
}

func NewSubject(id, categoryID string, name SubjectName, description *string, now time.Time) Subject {
	return Subject{ID: id, CategoryID: categoryID, Name: name, Description: description, CreatedAt: now, UpdatedAt: now}
}

// Delete is logical: the row stays, marked deleted (Spec 16 FR-0).
func (s *Subject) Delete(now time.Time) {
	s.DeletedAt = &now
	s.UpdatedAt = now
}

// CategoryRef is the slice of a category a subject exposes.
type CategoryRef struct {
	ID    string
	Name  string
	Color string
	Path  string
}

// SubjectView is the read model: the subject plus its category reference and
// the number of alive notes.
type SubjectView struct {
	Subject
	Category  CategoryRef
	NoteCount int
}
