package out

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// CategoryRepository persists the category tree. Every read sees alive rows
// only; nothing is ever hard-deleted (Spec 16 FR-0).
type CategoryRepository interface {
	// Create inserts a new category; a duplicate alive sibling name is a
	// *domain.ConflictError (duplicate_name).
	Create(ctx context.Context, cat domain.Category) error
	// Update persists every field of an alive category, including DeletedAt
	// for a logical delete. domain.ErrNotFound when the row is not alive.
	Update(ctx context.Context, cat domain.Category) error
	Get(ctx context.Context, id string) (domain.Category, error)
	GetView(ctx context.Context, id string) (domain.CategoryView, error)
	// List returns alive categories ordered by path segments (case-insensitive).
	List(ctx context.Context) ([]domain.CategoryView, error)
	// AncestorIDs returns the alive chain root → id, inclusive.
	AncestorIDs(ctx context.Context, id string) ([]string, error)
	// SubtreeHeight is 1 for a leaf, counting alive descendants only.
	SubtreeHeight(ctx context.Context, id string) (int, error)
	CountAliveChildren(ctx context.Context, id string) (int, error)
}
