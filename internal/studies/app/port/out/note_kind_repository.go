package out

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// NoteKindRepository persists note kinds. Every read sees alive rows only;
// nothing is ever hard-deleted (Spec 16 FR-0).
type NoteKindRepository interface {
	// Create inserts a new kind; a duplicate alive name is a
	// *domain.ConflictError (duplicate_name).
	Create(ctx context.Context, kind domain.NoteKind) error
	// Update persists every field of an alive kind, including DeletedAt for a
	// logical delete. domain.ErrNotFound when the row is not alive.
	Update(ctx context.Context, kind domain.NoteKind) error
	Get(ctx context.Context, id string) (domain.NoteKind, error)
	GetView(ctx context.Context, id string) (domain.NoteKindView, error)
	// List returns alive kinds ordered by case-folded name, then id.
	List(ctx context.Context) ([]domain.NoteKindView, error)
}
