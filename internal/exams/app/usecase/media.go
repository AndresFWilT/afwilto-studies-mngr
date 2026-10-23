package usecase

import (
	"context"
	"errors"

	"github.com/afwilto/studies-mngr/internal/exams/domain"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
)

var errNotImplemented = errors.New("not implemented")

type getMediaService struct {
	index   out.MediaIndex
	storage out.MediaStorage
}

func NewGetMediaService(index out.MediaIndex, storage out.MediaStorage) in.GetMedia {
	return &getMediaService{index: index, storage: storage}
}

func (s *getMediaService) Execute(ctx context.Context, sha string) (in.MediaContent, error) {
	return in.MediaContent{}, errNotImplemented
}

type exportQuestionsService struct {
	bank  out.QuestionBank
	index out.MediaIndex
}

func NewExportQuestionsService(bank out.QuestionBank, index out.MediaIndex) in.ExportQuestions {
	return &exportQuestionsService{bank: bank, index: index}
}

func (s *exportQuestionsService) Execute(ctx context.Context, examID string) (domain.QuestionExport, error) {
	return domain.QuestionExport{}, errNotImplemented
}
