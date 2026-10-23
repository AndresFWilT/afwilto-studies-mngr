package http

import (
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// questionSummaryDTO is the contract's QuestionSummary.
type questionSummaryDTO struct {
	ID          string                   `json:"id"`
	Key         string                   `json:"key"`
	Type        string                   `json:"type"`
	StemExcerpt string                   `json:"stemExcerpt"`
	Categories  []questionCategoryRefDTO `json:"categories"`
	Difficulty  *string                  `json:"difficulty"`
	Origin      *originOutDTO            `json:"origin"`
	Version     int                      `json:"version"`
	UpdatedAt   time.Time                `json:"updatedAt"`
}

func toQuestionSummaryDTO(s domain.QuestionSummary) questionSummaryDTO {
	dto := questionSummaryDTO{
		ID: s.ID, Key: s.Key, Type: string(s.Type), StemExcerpt: s.StemExcerpt,
		Categories: make([]questionCategoryRefDTO, len(s.Categories)),
		Difficulty: strPtr(s.Difficulty), Version: s.Version, UpdatedAt: s.UpdatedAt.UTC(),
	}
	for i, c := range s.Categories {
		dto.Categories[i] = questionCategoryRefDTO{Classification: c.Classification, Name: c.Category}
	}
	if s.Origin != nil {
		dto.Origin = &originOutDTO{Set: s.Origin.Set, Number: s.Origin.Number, NoteRef: s.Origin.NoteRef}
	}
	return dto
}

type mergeCategoriesRequest struct {
	From *string `json:"from"`
	Into *string `json:"into"`
}
