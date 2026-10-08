package in

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

type CreateCategoryCommand struct {
	Name     string
	Color    string
	ParentID *string
}

type CreateCategory interface {
	Execute(ctx context.Context, cmd CreateCategoryCommand) (domain.CategoryView, error)
}

type GetCategory interface {
	Execute(ctx context.Context, id string) (domain.CategoryView, error)
}

type ListCategories interface {
	Execute(ctx context.Context) ([]domain.CategoryView, error)
}

// UpdateCategoryCommand: ParentID set to null moves the category to the root.
type UpdateCategoryCommand struct {
	Name     Patch[string]
	Color    Patch[string]
	ParentID Patch[string]
}

type UpdateCategory interface {
	Execute(ctx context.Context, id string, cmd UpdateCategoryCommand) (domain.CategoryView, error)
}

type DeleteCategory interface {
	Execute(ctx context.Context, id string) error
}
