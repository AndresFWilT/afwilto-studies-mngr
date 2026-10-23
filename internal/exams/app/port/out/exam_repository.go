package out

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// ExamRepository persists exams. Every read sees alive rows only; nothing is
// ever hard-deleted (Spec 16 FR-0).
type ExamRepository interface {
	// Create inserts an exam. A duplicate alive name in the subject is a
	// *domain.ConflictError (duplicate_name); a missing or deleted subject is a
	// subjectId not_found validation error.
	Create(ctx context.Context, exam domain.Exam) error
	// Update persists the editable fields of an alive exam, but only when the
	// stored version equals expectedVersion. domain.ErrNotFound when the row is
	// not alive; a *domain.ConflictError (stale_version) when it is alive with
	// another version; duplicate_name when the new name clashes. The exam
	// carries the already-incremented version.
	Update(ctx context.Context, exam domain.Exam, expectedVersion int) error
	// Delete stamps DeletedAt of an alive exam without touching its version.
	// domain.ErrNotFound when the row is not alive.
	Delete(ctx context.Context, exam domain.Exam) error
	Get(ctx context.Context, id string) (domain.Exam, error)
	GetSummary(ctx context.Context, id string) (domain.ExamSummary, error)
	// ListBySubject returns the alive exams of a subject ordered by
	// case-folded name, then id.
	ListBySubject(ctx context.Context, subjectID string) ([]domain.ExamSummary, error)
}
