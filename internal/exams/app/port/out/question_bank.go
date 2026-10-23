package out

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// QuestionBank persists the classifications, categories and questions of an
// exam (Spec 18 §2). Every read sees alive rows only.
type QuestionBank interface {
	// LoadBank returns the alive classifications (with categories) and the
	// alive questions of an alive exam, in creation order; domain.ErrNotFound
	// when the exam is not alive.
	LoadBank(ctx context.Context, examID string) (domain.Bank, error)

	// ApplyImport is the all-or-nothing write of an import. It serializes
	// imports of the exam, loads the bank, calls plan with it and persists the
	// returned write in one transaction. When plan returns a nil write or an
	// error, nothing is written and the error is returned. domain.ErrNotFound
	// when the exam is not alive.
	ApplyImport(ctx context.Context, examID string, plan func(bank domain.Bank) (*domain.ImportWrite, error)) error

	// ListQuestions returns the bank browser rows of an alive exam, filtered
	// (all filters combined with AND) and sorted by origin set, origin number
	// and key, questions without an origin last. domain.ErrNotFound when the
	// exam is not alive; no match is an empty list.
	ListQuestions(ctx context.Context, examID string, filter domain.QuestionFilter) ([]domain.QuestionSummary, error)

	// GetQuestion returns a full alive question with its options in stored
	// order; domain.ErrNotFound when it is not alive.
	GetQuestion(ctx context.Context, id string) (domain.Question, error)

	// ApplyEdit is the all-or-nothing write of an exam edit, a category merge
	// or a question delete. It serializes with imports and other edits of the
	// exam, loads the exam row and the bank, calls plan with them and persists
	// the returned write in one transaction. When plan returns a nil write or
	// an error, nothing is written and the error is returned.
	// domain.ErrNotFound when the exam is not alive; a duplicate_name conflict
	// when a rename meets the unique name indexes.
	ApplyEdit(ctx context.Context, examID string, plan func(exam domain.Exam, bank domain.Bank) (*domain.EditWrite, error)) error
}
