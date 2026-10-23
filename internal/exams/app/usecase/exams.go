package usecase

import (
	"context"
	"errors"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

var errNotImplemented = errors.New("not implemented")

type createExamService struct {
	repo     out.ExamRepository
	subjects out.SubjectDirectory
	clock    out.Clock
	ids      out.IDGenerator
}

func NewCreateExamService(repo out.ExamRepository, subjects out.SubjectDirectory, clock out.Clock, ids out.IDGenerator) in.CreateExam {
	return &createExamService{repo: repo, subjects: subjects, clock: clock, ids: ids}
}

func (s *createExamService) Execute(ctx context.Context, cmd in.CreateExamCommand) (domain.ExamSummary, error) {
	return domain.ExamSummary{}, errNotImplemented
}

type getExamService struct{ repo out.ExamRepository }

func NewGetExamService(repo out.ExamRepository) in.GetExam { return &getExamService{repo: repo} }

func (s *getExamService) Execute(ctx context.Context, id string) (domain.ExamSummary, error) {
	return domain.ExamSummary{}, errNotImplemented
}

type listSubjectExamsService struct {
	repo     out.ExamRepository
	subjects out.SubjectDirectory
}

func NewListSubjectExamsService(repo out.ExamRepository, subjects out.SubjectDirectory) in.ListSubjectExams {
	return &listSubjectExamsService{repo: repo, subjects: subjects}
}

func (s *listSubjectExamsService) Execute(ctx context.Context, subjectID string) ([]domain.ExamSummary, error) {
	return nil, errNotImplemented
}

type updateExamService struct {
	repo  out.ExamRepository
	clock out.Clock
}

func NewUpdateExamService(repo out.ExamRepository, clock out.Clock) in.UpdateExam {
	return &updateExamService{repo: repo, clock: clock}
}

func (s *updateExamService) Execute(ctx context.Context, id string, cmd in.UpdateExamCommand) (domain.ExamSummary, error) {
	return domain.ExamSummary{}, errNotImplemented
}

type deleteExamService struct {
	repo  out.ExamRepository
	clock out.Clock
}

func NewDeleteExamService(repo out.ExamRepository, clock out.Clock) in.DeleteExam {
	return &deleteExamService{repo: repo, clock: clock}
}

func (s *deleteExamService) Execute(ctx context.Context, id string) error { return errNotImplemented }
