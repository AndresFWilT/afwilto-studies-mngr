package usecase

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// ── Bank browser ────────────────────────────────────────────────────────────

type listExamQuestionsService struct{ bank out.QuestionBank }

func NewListExamQuestionsService(bank out.QuestionBank) in.ListExamQuestions {
	return &listExamQuestionsService{bank: bank}
}

func (s *listExamQuestionsService) Execute(ctx context.Context, examID string, filter domain.QuestionFilter) ([]domain.QuestionSummary, error) {
	if violations := filter.Violations(); len(violations) > 0 {
		return nil, domain.NewValidation(violations...)
	}
	return s.bank.ListQuestions(ctx, examID, filter)
}

// ── Question read and delete ────────────────────────────────────────────────

type getQuestionService struct{ bank out.QuestionBank }

func NewGetQuestionService(bank out.QuestionBank) in.GetQuestion {
	return &getQuestionService{bank: bank}
}

func (s *getQuestionService) Execute(ctx context.Context, id string) (domain.Question, error) {
	return s.bank.GetQuestion(ctx, id)
}

type deleteQuestionService struct {
	bank  out.QuestionBank
	clock out.Clock
}

func NewDeleteQuestionService(bank out.QuestionBank, clock out.Clock) in.DeleteQuestion {
	return &deleteQuestionService{bank: bank, clock: clock}
}

// Execute finds the exam of the question, then deletes it and recomputes the
// weights inside the bank's transaction, so a question deleted in between is
// ErrNotFound rather than a second delete.
func (s *deleteQuestionService) Execute(ctx context.Context, id string) error {
	question, err := s.bank.GetQuestion(ctx, id)
	if err != nil {
		return err
	}
	return s.bank.ApplyEdit(ctx, question.ExamID, func(exam domain.Exam, bank domain.Bank) (*domain.EditWrite, error) {
		write, err := domain.PlanDeleteQuestion(exam, bank, id, s.clock.Now())
		if err != nil {
			return nil, err
		}
		return &write, nil
	})
}

// ── Category merge ──────────────────────────────────────────────────────────

type mergeCategoriesService struct {
	repo  out.ExamRepository
	bank  out.QuestionBank
	clock out.Clock
}

func NewMergeCategoriesService(repo out.ExamRepository, bank out.QuestionBank, clock out.Clock) in.MergeCategories {
	return &mergeCategoriesService{repo: repo, bank: bank, clock: clock}
}

func (s *mergeCategoriesService) Execute(ctx context.Context, examID string, cmd in.MergeCategoriesCommand) (domain.ExamSummary, error) {
	err := s.bank.ApplyEdit(ctx, examID, func(exam domain.Exam, bank domain.Bank) (*domain.EditWrite, error) {
		write, err := domain.PlanMerge(exam, bank, cmd.From, cmd.Into, s.clock.Now())
		if err != nil {
			return nil, err
		}
		return &write, nil
	})
	if err != nil {
		return domain.ExamSummary{}, err
	}
	return s.repo.GetSummary(ctx, examID)
}
