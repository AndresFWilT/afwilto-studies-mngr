package in

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

type CreateSubjectCommand struct {
	CategoryID  string
	Name        string
	Description *string
}

type CreateSubject interface {
	Execute(ctx context.Context, cmd CreateSubjectCommand) (domain.SubjectView, error)
}

type GetSubject interface {
	Execute(ctx context.Context, id string) (domain.SubjectView, error)
}

// ListSubjects filters to the direct members of CategoryID when it is set.
type ListSubjects interface {
	Execute(ctx context.Context, categoryID *string) ([]domain.SubjectView, error)
}

// UpdateSubjectCommand: Description set to null clears it; CategoryID and Name
// set to null are validation errors.
type UpdateSubjectCommand struct {
	CategoryID  Patch[string]
	Name        Patch[string]
	Description Patch[string]
}

type UpdateSubject interface {
	Execute(ctx context.Context, id string, cmd UpdateSubjectCommand) (domain.SubjectView, error)
}

type DeleteSubject interface {
	Execute(ctx context.Context, id string) error
}
