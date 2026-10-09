package in

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

type CreateNoteKindCommand struct {
	Name string
}

type CreateNoteKind interface {
	Execute(ctx context.Context, cmd CreateNoteKindCommand) (domain.NoteKindView, error)
}

type GetNoteKind interface {
	Execute(ctx context.Context, id string) (domain.NoteKindView, error)
}

type ListNoteKinds interface {
	Execute(ctx context.Context) ([]domain.NoteKindView, error)
}

type RenameNoteKindCommand struct {
	Name string
}

type RenameNoteKind interface {
	Execute(ctx context.Context, id string, cmd RenameNoteKindCommand) (domain.NoteKindView, error)
}

type DeleteNoteKind interface {
	Execute(ctx context.Context, id string) error
}
