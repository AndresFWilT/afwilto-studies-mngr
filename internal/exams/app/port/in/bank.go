package in

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// ListExamQuestions is the bank browser. A filter value that is not allowed
// is a validation error; domain.ErrNotFound when the exam is not alive.
type ListExamQuestions interface {
	Execute(ctx context.Context, examID string, filter domain.QuestionFilter) ([]domain.QuestionSummary, error)
}

// GetQuestion returns the full question; domain.ErrNotFound when it is not alive.
type GetQuestion interface {
	Execute(ctx context.Context, id string) (domain.Question, error)
}

// DeleteQuestion is a logical delete that recomputes the inferred weights;
// domain.ErrNotFound when the question is not alive.
type DeleteQuestion interface {
	Execute(ctx context.Context, id string) error
}

type MergeCategoriesCommand struct{ From, Into string }

// MergeCategories moves the questions of From into Into and deletes From
// (BR-C5); the exam version increments.
type MergeCategories interface {
	Execute(ctx context.Context, examID string, cmd MergeCategoriesCommand) (domain.ExamSummary, error)
}
