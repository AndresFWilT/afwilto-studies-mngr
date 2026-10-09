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
}

type CreateNote interface {
	Execute(ctx context.Context, cmd CreateNoteCommand) (domain.NoteView, error)
}

type GetNote interface {
	Execute(ctx context.Context, id string) (domain.NoteView, error)
}

// ListSubjectNotes returns summaries (empty Content) of a subject's alive notes.
type ListSubjectNotes interface {
	Execute(ctx context.Context, subjectID string, sort domain.SortSpec) ([]domain.NoteView, error)
}

// UpdateNoteCommand: Version is the version the client read. Every patch field
// set to null is a validation error. SubjectID moves the note.
type UpdateNoteCommand struct {
	Version   int
	Title     Patch[string]
	KindID    Patch[string]
	SubjectID Patch[string]
	Content   Patch[string]
}

type UpdateNote interface {
	Execute(ctx context.Context, id string, cmd UpdateNoteCommand) (domain.NoteView, error)
}

type DeleteNote interface {
	Execute(ctx context.Context, id string) error
}
