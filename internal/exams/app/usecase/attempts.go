package usecase

import (
	"context"
	"errors"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

var errNotImplemented = errors.New("not implemented")

type previewSessionService struct {
	exams out.ExamRepository
	bank  out.QuestionBank
}

func NewPreviewSessionService(exams out.ExamRepository, bank out.QuestionBank) in.PreviewSession {
	return &previewSessionService{exams: exams, bank: bank}
}

func (s *previewSessionService) Execute(ctx context.Context, examID string, cfg domain.SessionConfig) (domain.SessionPreview, error) {
	return domain.SessionPreview{}, errNotImplemented
}

type startAttemptService struct {
	exams    out.ExamRepository
	bank     out.QuestionBank
	attempts out.AttemptRepository
	clock    out.Clock
	ids      out.IDGenerator
	seeds    out.SeedSource
}

func NewStartAttemptService(exams out.ExamRepository, bank out.QuestionBank, attempts out.AttemptRepository, clock out.Clock, ids out.IDGenerator, seeds out.SeedSource) in.StartAttempt {
	return &startAttemptService{exams: exams, bank: bank, attempts: attempts, clock: clock, ids: ids, seeds: seeds}
}

func (s *startAttemptService) Execute(ctx context.Context, examID string, cmd in.StartAttemptCommand) (domain.AttemptView, error) {
	return domain.AttemptView{}, errNotImplemented
}
