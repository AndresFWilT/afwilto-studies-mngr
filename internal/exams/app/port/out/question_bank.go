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
}
