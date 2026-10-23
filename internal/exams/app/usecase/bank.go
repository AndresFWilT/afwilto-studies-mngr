package usecase

import (
	"context"
	"errors"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

var errNotImplemented = errors.New("not implemented")

type listExamQuestionsService struct{ bank out.QuestionBank }

func NewListExamQuestionsService(bank out.QuestionBank) in.ListExamQuestions {
	return &listExamQuestionsService{bank: bank}
}

func (s *listExamQuestionsService) Execute(ctx context.Context, examID string, filter domain.QuestionFilter) ([]domain.QuestionSummary, error) {
	return nil, errNotImplemented
}

type getQuestionService struct{ bank out.QuestionBank }

func NewGetQuestionService(bank out.QuestionBank) in.GetQuestion {
	return &getQuestionService{bank: bank}
}

func (s *getQuestionService) Execute(ctx context.Context, id string) (domain.Question, error) {
	return domain.Question{}, errNotImplemented
}

type deleteQuestionService struct {
	bank  out.QuestionBank
	clock out.Clock
}

func NewDeleteQuestionService(bank out.QuestionBank, clock out.Clock) in.DeleteQuestion {
	return &deleteQuestionService{bank: bank, clock: clock}
}

func (s *deleteQuestionService) Execute(ctx context.Context, id string) error {
	return errNotImplemented
}

type mergeCategoriesService struct {
	repo  out.ExamRepository
	bank  out.QuestionBank
	clock out.Clock
}

func NewMergeCategoriesService(repo out.ExamRepository, bank out.QuestionBank, clock out.Clock) in.MergeCategories {
	return &mergeCategoriesService{repo: repo, bank: bank, clock: clock}
}

func (s *mergeCategoriesService) Execute(ctx context.Context, examID string, cmd in.MergeCategoriesCommand) (domain.ExamSummary, error) {
	return domain.ExamSummary{}, errNotImplemented
}
