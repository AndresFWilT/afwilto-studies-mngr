package usecase

import (
	"context"
	"errors"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

var errNotImplemented = errors.New("not implemented")

type getAttemptService struct{}

func NewGetAttemptService(attempts out.AttemptRepository, clock out.Clock) in.GetAttempt {
	return &getAttemptService{}
}

func (s *getAttemptService) Execute(context.Context, string) (domain.AttemptView, error) {
	return domain.AttemptView{}, errNotImplemented
}

type answerItemService struct{}

func NewAnswerItemService(attempts out.AttemptRepository, clock out.Clock) in.AnswerItem {
	return &answerItemService{}
}

func (s *answerItemService) Execute(context.Context, string, int, domain.AnswerInput) (domain.AnswerOutcome, error) {
	return domain.AnswerOutcome{}, errNotImplemented
}

type submitAttemptService struct{}

func NewSubmitAttemptService(attempts out.AttemptRepository, clock out.Clock) in.SubmitAttempt {
	return &submitAttemptService{}
}

func (s *submitAttemptService) Execute(context.Context, string) (domain.AttemptResult, error) {
	return domain.AttemptResult{}, errNotImplemented
}

type reviewAttemptService struct{}

func NewReviewAttemptService(attempts out.AttemptRepository, notes out.NoteDirectory, clock out.Clock) in.ReviewAttempt {
	return &reviewAttemptService{}
}

func (s *reviewAttemptService) Execute(context.Context, string) ([]domain.ReviewItem, error) {
	return nil, errNotImplemented
}

type listExamAttemptsService struct{}

func NewListExamAttemptsService(exams out.ExamRepository, attempts out.AttemptRepository) in.ListExamAttempts {
	return &listExamAttemptsService{}
}

func (s *listExamAttemptsService) Execute(context.Context, string) (domain.AttemptHistory, error) {
	return domain.AttemptHistory{}, errNotImplemented
}
