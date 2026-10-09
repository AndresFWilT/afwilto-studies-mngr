package out

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// SubjectRepository persists subjects. Every read sees alive rows only;
// nothing is ever hard-deleted (Spec 16 FR-0).
type SubjectRepository interface {
	// Create inserts a subject. A duplicate alive name in the category is a
	// *domain.ConflictError (duplicate_name); a missing or deleted category is
	// a categoryId not_found validation error.
	Create(ctx context.Context, subject domain.Subject) error
	// Update persists every field of an alive subject, including CategoryID
	// (a move) and DeletedAt (a logical delete). domain.ErrNotFound when the
	// row is not alive.
	Update(ctx context.Context, subject domain.Subject) error
	Get(ctx context.Context, id string) (domain.Subject, error)
	GetView(ctx context.Context, id string) (domain.SubjectView, error)
	// List returns alive subjects ordered by case-folded name, then id. A
	// non-nil categoryID restricts it to that category's direct members.
	List(ctx context.Context, categoryID *string) ([]domain.SubjectView, error)
	CountAliveInCategory(ctx context.Context, categoryID string) (int, error)
}
