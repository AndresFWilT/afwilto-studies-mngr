package in

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// CreateNoteCommand: a nil Content means the empty document.
type CreateNoteCommand struct {
	SubjectID string
	Title     string
	KindID    string
	Content   *string
	Tags      []string
	SlabLinks []domain.SlabLinkInput
}

type CreateNote interface {
	Execute(ctx context.Context, cmd CreateNoteCommand) (domain.NoteView, error)
}

type GetNote interface {
	Execute(ctx context.Context, id string) (domain.NoteView, error)
}

// ListSubjectNotes returns summaries (empty Content) of a subject's alive notes,
// optionally only those carrying the raw tag (normalized by the use case).
type ListSubjectNotes interface {
	Execute(ctx context.Context, subjectID string, tag *string, sort domain.SortSpec) ([]domain.NoteView, error)
}

// ListNotesByTag returns summaries of the alive notes carrying the raw tag in
// every subject, each with its subject and category.
type ListNotesByTag interface {
	Execute(ctx context.Context, tag string, sort domain.SortSpec) ([]domain.NoteView, error)
}

// ListTags returns the tags with at least one alive note, by name.
type ListTags interface {
	Execute(ctx context.Context) ([]domain.TagCount, error)
}

// UpdateNoteCommand: Version is the version the client read. Every patch field
// set to null is a validation error. SubjectID moves the note. Tags and
// SlabLinks replace the whole set / list.
type UpdateNoteCommand struct {
	Version   int
	Title     Patch[string]
	KindID    Patch[string]
	SubjectID Patch[string]
	Content   Patch[string]
	Tags      Patch[[]string]
	SlabLinks Patch[[]domain.SlabLinkInput]
}

type UpdateNote interface {
	Execute(ctx context.Context, id string, cmd UpdateNoteCommand) (domain.NoteView, error)
}

type DeleteNote interface {
	Execute(ctx context.Context, id string) error
}
