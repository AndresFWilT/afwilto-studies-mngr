package out

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// NoteRepository persists notes. Every read sees alive rows only; nothing is
// ever hard-deleted (Spec 16 FR-0).
type NoteRepository interface {
	// Create inserts a note. A duplicate alive title in the subject is a
	// *domain.ConflictError (duplicate_name); a missing or deleted subject or
	// kind is a subjectId / kindId not_found validation error.
	Create(ctx context.Context, note domain.Note) error
	// Update persists the editable fields of an alive note, but only when the
	// stored version equals expectedVersion. domain.ErrNotFound when the row is
	// not alive; a *domain.ConflictError (stale_version) when it is alive with
	// another version. The note carries the already-incremented version.
	Update(ctx context.Context, note domain.Note, expectedVersion int) error
	// Delete stamps DeletedAt of an alive note without touching its version.
	// domain.ErrNotFound when the row is not alive.
	Delete(ctx context.Context, note domain.Note) error
	Get(ctx context.Context, id string) (domain.Note, error)
	GetView(ctx context.Context, id string) (domain.NoteView, error)
	// ListBySubject returns summaries (empty Content) ordered by sort, ties by
	// title ascending then id ascending.
	ListBySubject(ctx context.Context, subjectID string, sort domain.SortSpec) ([]domain.NoteView, error)
	CountAliveBySubject(ctx context.Context, subjectID string) (int, error)
	CountAliveByKind(ctx context.Context, kindID string) (int, error)
}
