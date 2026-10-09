package domain

import (
	"strings"
	"time"
	"unicode"
)

const maxNoteKindNameRunes = 40

// NoteKindName is trimmed and 1–40 runes long. Uniqueness among alive kinds
// compares Key(), the case-folded form.
type NoteKindName struct{ value string }

func NewNoteKindName(raw string) (NoteKindName, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return NoteKindName{}, SingleViolation("name", "required", "name must not be empty")
	}
	if strings.ContainsFunc(trimmed, unicode.IsControl) {
		return NoteKindName{}, SingleViolation("name", "format", "name must not contain control characters")
	}
	if len([]rune(trimmed)) > maxNoteKindNameRunes {
		return NoteKindName{}, SingleViolation("name", "max_length", "name must be at most 40 characters")
	}
	return NoteKindName{value: trimmed}, nil
}

// RehydrateNoteKindName trusts persisted data.
func RehydrateNoteKindName(value string) NoteKindName { return NoteKindName{value: value} }

func (n NoteKindName) String() string { return n.value }
func (n NoteKindName) Key() string    { return strings.ToLower(n.value) }

// NoteKind classifies a note (Summary, Cheat sheet, Notes, …). It is a managed
// resource, not an enum (Spec 16 FR-7).
type NoteKind struct {
	ID        string
	Name      NoteKindName
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

func NewNoteKind(id string, name NoteKindName, now time.Time) NoteKind {
	return NoteKind{ID: id, Name: name, CreatedAt: now, UpdatedAt: now}
}

// Delete is logical: the row stays, marked deleted (Spec 16 FR-0).
func (k *NoteKind) Delete(now time.Time) {
	k.DeletedAt = &now
	k.UpdatedAt = now
}

// NoteKindView is the read model: the kind plus the number of alive notes
// that use it.
type NoteKindView struct {
	NoteKind
	NoteCount int
}
