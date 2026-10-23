package in

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

type CreateExamCommand struct {
	SubjectID     string
	Name          string
	QuestionCount int
	TimeLimitMin  int
	PassPercent   int
}

// CreateExam makes an exam on an alive subject; an unknown or deleted subject
// is a subjectId not_found validation error, not a 404.
type CreateExam interface {
	Execute(ctx context.Context, cmd CreateExamCommand) (domain.ExamSummary, error)
}

type GetExam interface {
	Execute(ctx context.Context, id string) (domain.ExamSummary, error)
}

// ListSubjectExams lists the alive exams of a subject by name; domain.ErrNotFound
// when the subject is not alive.
type ListSubjectExams interface {
	Execute(ctx context.Context, subjectID string) ([]domain.ExamSummary, error)
}

// UpdateExamCommand: Version is the version the client read; Edit holds the
// fields to change (nil means unchanged) and Structure the classification and
// category edits (slice 04). At least one of them must change something. All
// of it is applied atomically.
type UpdateExamCommand struct {
	Version   int
	Edit      domain.ExamEdit
	Structure domain.StructureEdit
}

type UpdateExam interface {
	Execute(ctx context.Context, id string, cmd UpdateExamCommand) (domain.ExamSummary, error)
}

type DeleteExam interface {
	Execute(ctx context.Context, id string) error
}
