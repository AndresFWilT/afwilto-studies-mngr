package usecase

import (
	"context"
	"errors"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

type importQuestionsService struct {
	bank  out.QuestionBank
	clock out.Clock
	ids   out.IDGenerator
}

func NewImportQuestionsService(bank out.QuestionBank, clock out.Clock, ids out.IDGenerator) in.ImportQuestions {
	return &importQuestionsService{bank: bank, clock: clock, ids: ids}
}

// Execute — slice 03 red stub.
func (s *importQuestionsService) Execute(ctx context.Context, cmd in.ImportQuestionsCommand) (domain.ImportReport, error) {
	return domain.ImportReport{}, errors.New("not implemented")
}
