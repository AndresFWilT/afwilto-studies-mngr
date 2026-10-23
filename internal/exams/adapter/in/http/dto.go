package http

import (
	"encoding/json"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// Opt distinguishes absent (Set=false) from explicit null (Set=true,
// Value=nil) so PATCH can tell "leave it" from "clear it".
type Opt[T any] struct {
	Set   bool
	Value *T
}

func (o *Opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	o.Value = &v
	return nil
}

// null reports an explicit JSON null.
func (o Opt[T]) null() bool { return o.Set && o.Value == nil }

type listMeta struct {
	TotalItems int `json:"totalItems"`
}

// ── Exams ───────────────────────────────────────────────────────────────────

// createExamRequest uses pointers so a missing field is "required" rather than
// a misleading range error on 0.
type createExamRequest struct {
	Name          *string `json:"name"`
	QuestionCount *int    `json:"questionCount"`
	TimeLimitMin  *int    `json:"timeLimitMin"`
	PassPercent   *int    `json:"passPercent"`
}

// updateExamRequest carries the slice 02 fields; the classification and
// category fields of the contract arrive with slice 04.
type updateExamRequest struct {
	Version       *int        `json:"version"`
	Name          Opt[string] `json:"name"`
	QuestionCount Opt[int]    `json:"questionCount"`
	TimeLimitMin  Opt[int]    `json:"timeLimitMin"`
	PassPercent   Opt[int]    `json:"passPercent"`
}

type lastSimulationDTO struct {
	ScorePercent float64   `json:"scorePercent"`
	Passed       bool      `json:"passed"`
	At           time.Time `json:"at"`
}

// examSummaryDTO is the contract's ExamSummary.
type examSummaryDTO struct {
	ID             string             `json:"id"`
	SubjectID      string             `json:"subjectId"`
	Name           string             `json:"name"`
	QuestionCount  int                `json:"questionCount"`
	TimeLimitMin   int                `json:"timeLimitMin"`
	PassPercent    int                `json:"passPercent"`
	QuestionTotal  int                `json:"questionTotal"`
	LastSimulation *lastSimulationDTO `json:"lastSimulation,omitempty"`
	Version        int                `json:"version"`
	CreatedAt      time.Time          `json:"createdAt"`
	UpdatedAt      time.Time          `json:"updatedAt"`
}

// examDTO is the contract's Exam: the summary plus classifications, which stay
// empty until slice 03 creates them.
type examDTO struct {
	examSummaryDTO
	Classifications []any `json:"classifications"`
}

func toExamSummaryDTO(s domain.ExamSummary) examSummaryDTO {
	dto := examSummaryDTO{
		ID:            s.ID,
		SubjectID:     s.SubjectID,
		Name:          s.Name.String(),
		QuestionCount: s.QuestionCount,
		TimeLimitMin:  s.TimeLimitMin,
		PassPercent:   s.PassPercent,
		QuestionTotal: s.QuestionTotal,
		Version:       s.Version,
		CreatedAt:     s.CreatedAt.UTC(),
		UpdatedAt:     s.UpdatedAt.UTC(),
	}
	if s.LastSimulation != nil {
		dto.LastSimulation = &lastSimulationDTO{
			ScorePercent: s.LastSimulation.ScorePercent, Passed: s.LastSimulation.Passed, At: s.LastSimulation.At.UTC(),
		}
	}
	return dto
}

func toExamDTO(s domain.ExamSummary) examDTO {
	return examDTO{examSummaryDTO: toExamSummaryDTO(s), Classifications: []any{}}
}
