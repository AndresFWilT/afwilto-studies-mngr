package usecase

import (
	"context"
	"fmt"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

type getMediaService struct {
	index   out.MediaIndex
	storage out.MediaStorage
}

func NewGetMediaService(index out.MediaIndex, storage out.MediaStorage) in.GetMedia {
	return &getMediaService{index: index, storage: storage}
}

func (s *getMediaService) Execute(ctx context.Context, sha string) (in.MediaContent, error) {
	if !domain.IsMediaSHA(sha) { // never reaches the index or the disk
		return in.MediaContent{}, domain.ErrNotFound
	}
	found, err := s.index.Find(ctx, []string{sha})
	if err != nil {
		return in.MediaContent{}, err
	}
	media, ok := found[sha]
	if !ok {
		return in.MediaContent{}, domain.ErrNotFound
	}
	body, err := s.storage.Open(ctx, sha)
	if err != nil { // a row without its file is a server fault, not a 404
		return in.MediaContent{}, fmt.Errorf("opening stored media %s: %w", sha, err)
	}
	return in.MediaContent{Media: media, Body: body}, nil
}

type exportQuestionsService struct {
	bank  out.QuestionBank
	index out.MediaIndex
}

func NewExportQuestionsService(bank out.QuestionBank, index out.MediaIndex) in.ExportQuestions {
	return &exportQuestionsService{bank: bank, index: index}
}

func (s *exportQuestionsService) Execute(ctx context.Context, examID string) (domain.QuestionExport, error) {
	bank, err := s.bank.LoadBank(ctx, examID)
	if err != nil {
		return domain.QuestionExport{}, err
	}
	hashes := domain.QuestionMediaHashes(bank.Questions)
	media, err := s.index.Find(ctx, hashes)
	if err != nil {
		return domain.QuestionExport{}, fmt.Errorf("looking up exported media: %w", err)
	}
	for _, sha := range hashes {
		if _, ok := media[sha]; !ok {
			return domain.QuestionExport{}, fmt.Errorf("a question references media %s, which has no row", sha)
		}
	}
	return domain.QuestionExport{Questions: bank.Questions, Media: media}, nil
}
