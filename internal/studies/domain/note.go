package domain

import (
	"strings"
	"time"
	"unicode"
)

const (
	maxNoteTitleRunes     = 200
	maxMarkdownContentLen = 200_000
)

// NoteTitle is trimmed and 1–200 runes long. Uniqueness among the alive notes
// of a subject compares Key(), the case-folded form.
type NoteTitle struct{ value string }

func NewNoteTitle(raw string) (NoteTitle, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return NoteTitle{}, SingleViolation("title", "required", "title must not be empty")
	}
	if strings.ContainsFunc(trimmed, unicode.IsControl) {
		return NoteTitle{}, SingleViolation("title", "format", "title must not contain control characters")
	}
	if len([]rune(trimmed)) > maxNoteTitleRunes {
		return NoteTitle{}, SingleViolation("title", "max_length", "title must be at most 200 characters")
	}
	return NoteTitle{value: trimmed}, nil
}

// RehydrateNoteTitle trusts persisted data.
func RehydrateNoteTitle(value string) NoteTitle { return NoteTitle{value: value} }

func (t NoteTitle) String() string { return t.value }
func (t NoteTitle) Key() string    { return strings.ToLower(t.value) }

// MarkdownContent is the note body: 0–200,000 characters, kept verbatim. Only
// NUL is rejected, because Postgres text cannot store it.
type MarkdownContent struct{ value string }

func NewMarkdownContent(raw string) (MarkdownContent, error) {
	if strings.ContainsRune(raw, 0) {
		return MarkdownContent{}, SingleViolation("content", "format", "content must not contain NUL characters")
	}
	// Bytes bound the rune count from above, so the common case skips counting.
	if len(raw) > maxMarkdownContentLen && len([]rune(raw)) > maxMarkdownContentLen {
		return MarkdownContent{}, SingleViolation("content", "max_length", "content must be at most 200000 characters")
	}
	return MarkdownContent{value: raw}, nil
}

// RehydrateMarkdownContent trusts persisted data.
func RehydrateMarkdownContent(value string) MarkdownContent { return MarkdownContent{value: value} }

func (c MarkdownContent) String() string { return c.value }

// SortField names the column a note list is ordered by.
type SortField string

const (
	SortByTitle     SortField = "title"
	SortByCreatedAt SortField = "createdAt"
	SortByUpdatedAt SortField = "updatedAt"
)

// SortSpec orders a note list. Ties always break by title ascending, then id
// ascending, so the order is deterministic (Spec 16 FR-3).
type SortSpec struct {
	Field SortField
	Desc  bool
}

// ParseSortSpec reads the sort and order query values; empty means default
// (title, ascending).
func ParseSortSpec(sort, order string) (SortSpec, error) {
	spec := SortSpec{Field: SortByTitle}
	switch SortField(sort) {
	case "":
	case SortByTitle, SortByCreatedAt, SortByUpdatedAt:
		spec.Field = SortField(sort)
	default:
		return SortSpec{}, SingleViolation("sort", "enum", "sort must be one of title, createdAt, updatedAt")
	}
	switch order {
	case "", "asc":
	case "desc":
		spec.Desc = true
	default:
		return SortSpec{}, SingleViolation("order", "enum", "order must be asc or desc")
	}
	return spec, nil
}

// Note is a Markdown document that belongs to a subject. Version guards
// against silent overwrites from a stale editor (BR-NOTE3).
type Note struct {
	ID        string
	SubjectID string
	KindID    string
	Title     NoteTitle
	Content   MarkdownContent
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

func NewNote(id, subjectID, kindID string, title NoteTitle, content MarkdownContent, now time.Time) Note {
	return Note{ID: id, SubjectID: subjectID, KindID: kindID, Title: title, Content: content,
		Version: 1, CreatedAt: now, UpdatedAt: now}
}

// Touch records a successful edit: version + 1, updatedAt = now (BR-NOTE4).
func (n *Note) Touch(now time.Time) {
	n.Version++
	n.UpdatedAt = now
}

// Delete is logical: the row stays, marked deleted (Spec 16 FR-0). The
// version is left alone.
func (n *Note) Delete(now time.Time) {
	n.DeletedAt = &now
}

// NoteKindRef is the slice of a note kind a note exposes.
type NoteKindRef struct {
	ID   string
	Name string
}

// NoteView is the read model. Content is empty in list results. Tags arrive
// in Slice 06.
type NoteView struct {
	Note
	Kind NoteKindRef
	Tags []string
}
