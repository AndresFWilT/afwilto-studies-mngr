package http

import (
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// ── ImportReport (contract schema ImportReport) ─────────────────────────────

type importErrorDTO struct {
	Path    string `json:"path"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

type countsDTO struct {
	Create    int `json:"create"`
	Update    int `json:"update"`
	Unchanged int `json:"unchanged"`
	Invalid   int `json:"invalid"`
}

type keysDTO struct {
	Create    []string `json:"create"`
	Update    []string `json:"update"`
	Unchanged []string `json:"unchanged"`
}

type newClassificationDTO struct {
	Name string `json:"name"`
}

type newCategoryDTO struct {
	Classification string   `json:"classification"`
	Name           string   `json:"name"`
	SimilarTo      []string `json:"similarTo"`
}

type weightAfterDTO struct {
	Category string `json:"category"`
	Percent  int    `json:"percent"`
	Mode     string `json:"mode"`
}

type duplicateStemDTO struct {
	Key         string `json:"key"`
	ExistingKey string `json:"existingKey"`
}

type mediaReportDTO struct {
	Referenced int      `json:"referenced"`
	InZip      int      `json:"inZip"`
	Missing    []string `json:"missing"`
	Unused     []string `json:"unused"`
}

type importReportDTO struct {
	DryRun             bool                   `json:"dryRun"`
	Applied            bool                   `json:"applied"`
	Counts             countsDTO              `json:"counts"`
	Keys               keysDTO                `json:"keys"`
	Errors             []importErrorDTO       `json:"errors"`
	Primary            *string                `json:"primary"`
	NewClassifications []newClassificationDTO `json:"newClassifications"`
	NewCategories      []newCategoryDTO       `json:"newCategories"`
	WeightsAfter       []weightAfterDTO       `json:"weightsAfter"`
	DuplicateStems     []duplicateStemDTO     `json:"duplicateStems"`
	Media              mediaReportDTO         `json:"media"`
	Preview            []questionDTO          `json:"preview"`
	Warnings           []string               `json:"warnings"`
}

// nonNil keeps JSON arrays arrays: the contract never allows null for them.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func toImportReportDTO(r domain.ImportReport) importReportDTO {
	dto := importReportDTO{
		DryRun: r.DryRun, Applied: r.Applied,
		Counts:   countsDTO{Create: r.Counts.Create, Update: r.Counts.Update, Unchanged: r.Counts.Unchanged, Invalid: r.Counts.Invalid},
		Keys:     keysDTO{Create: nonNil(r.Keys.Create), Update: nonNil(r.Keys.Update), Unchanged: nonNil(r.Keys.Unchanged)},
		Primary:  r.Primary,
		Media:    mediaReportDTO{Referenced: r.Media.Referenced, InZip: r.Media.InZip, Missing: nonNil(r.Media.Missing), Unused: nonNil(r.Media.Unused)},
		Warnings: nonNil(r.Warnings),
	}
	dto.Errors = make([]importErrorDTO, len(r.Errors))
	for i, e := range r.Errors {
		dto.Errors[i] = importErrorDTO{Path: e.Path, Rule: e.Rule, Message: e.Message}
	}
	dto.NewClassifications = make([]newClassificationDTO, len(r.NewClassifications))
	for i, n := range r.NewClassifications {
		dto.NewClassifications[i] = newClassificationDTO{Name: n}
	}
	dto.NewCategories = make([]newCategoryDTO, len(r.NewCategories))
	for i, c := range r.NewCategories {
		dto.NewCategories[i] = newCategoryDTO{Classification: c.Classification, Name: c.Name, SimilarTo: nonNil(c.SimilarTo)}
	}
	dto.WeightsAfter = make([]weightAfterDTO, len(r.WeightsAfter))
	for i, w := range r.WeightsAfter {
		dto.WeightsAfter[i] = weightAfterDTO{Category: w.Category, Percent: w.Percent, Mode: string(w.Mode)}
	}
	dto.DuplicateStems = make([]duplicateStemDTO, len(r.DuplicateStems))
	for i, d := range r.DuplicateStems {
		dto.DuplicateStems[i] = duplicateStemDTO{Key: d.Key, ExistingKey: d.ExistingKey}
	}
	dto.Preview = make([]questionDTO, len(r.Preview))
	for i, q := range r.Preview {
		dto.Preview[i] = toQuestionDTO(q)
	}
	return dto
}

// ── Question (contract schema Question) ─────────────────────────────────────

type sourceOutDTO struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type originOutDTO struct {
	Set     string `json:"set,omitempty"`
	Number  int    `json:"number,omitempty"`
	NoteRef string `json:"noteRef,omitempty"`
}

type questionCategoryRefDTO struct {
	Classification string `json:"classification"`
	Name           string `json:"name"`
}

type questionOptionDTO struct {
	Position  int    `json:"position"`
	Label     string `json:"label"`
	Text      string `json:"text,omitempty"`
	Image     string `json:"image,omitempty"`
	Alt       string `json:"alt,omitempty"`
	Correct   bool   `json:"correct"`
	Rationale string `json:"rationale,omitempty"`
}

type questionDTO struct {
	ID          string                   `json:"id"`
	ExamID      string                   `json:"examId"`
	Key         string                   `json:"key"`
	Type        string                   `json:"type"`
	Stem        string                   `json:"stem"`
	Explanation *string                  `json:"explanation"`
	Difficulty  *string                  `json:"difficulty"`
	Source      sourceOutDTO             `json:"source"`
	Origin      *originOutDTO            `json:"origin"`
	Categories  []questionCategoryRefDTO `json:"categories"`
	Options     []questionOptionDTO      `json:"options"`
	Version     int                      `json:"version"`
	CreatedAt   time.Time                `json:"createdAt"`
	UpdatedAt   time.Time                `json:"updatedAt"`
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func toQuestionDTO(q domain.Question) questionDTO {
	dto := questionDTO{
		ID: q.ID, ExamID: q.ExamID, Key: q.Key, Type: string(q.Type), Stem: q.Stem,
		Explanation: strPtr(q.Explanation), Difficulty: strPtr(q.Difficulty),
		Source:     sourceOutDTO{Kind: q.Source.Kind, Name: q.Source.Name},
		Categories: make([]questionCategoryRefDTO, len(q.Categories)),
		Options:    make([]questionOptionDTO, len(q.Options)),
		Version:    q.Version, CreatedAt: q.CreatedAt.UTC(), UpdatedAt: q.UpdatedAt.UTC(),
	}
	if q.Origin != nil {
		dto.Origin = &originOutDTO{Set: q.Origin.Set, Number: q.Origin.Number, NoteRef: q.Origin.NoteRef}
	}
	for i, c := range q.Categories {
		dto.Categories[i] = questionCategoryRefDTO{Classification: c.Classification, Name: c.Category}
	}
	for i, o := range q.Options {
		dto.Options[i] = questionOptionDTO{Position: i, Label: string(rune('A' + i)), Text: o.Text, Image: o.Image, Alt: o.Alt, Correct: o.Correct, Rationale: o.Rationale}
	}
	return dto
}

// ── Exam classifications (contract schemas Classification and ExamCategory) ─

type examCategoryDTO struct {
	ID            string `json:"id"`
	Key           string `json:"key"`
	Name          string `json:"name"`
	WeightPercent *int   `json:"weightPercent"`
	PaceQuestions *int   `json:"paceQuestions"`
	PaceMinutes   *int   `json:"paceMinutes"`
	QuestionCount int    `json:"questionCount"`
}

type classificationDTO struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Position   int               `json:"position"`
	WeightMode string            `json:"weightMode"`
	Categories []examCategoryDTO `json:"categories"`
}

func toClassificationDTOs(cs []domain.Classification) []classificationDTO {
	out := make([]classificationDTO, len(cs))
	for i, c := range cs {
		dto := classificationDTO{ID: c.ID, Name: c.Name, Position: c.Position, WeightMode: string(c.WeightMode), Categories: make([]examCategoryDTO, len(c.Categories))}
		for j, k := range c.Categories {
			cat := examCategoryDTO{ID: k.ID, Key: k.Key, Name: k.Name, PaceQuestions: k.PaceQuestions, PaceMinutes: k.PaceMinutes, QuestionCount: k.QuestionCount}
			if c.IsPrimary() { // weights exist on the primary classification only
				w := k.WeightPercent
				cat.WeightPercent = &w
			}
			dto.Categories[j] = cat
		}
		out[i] = dto
	}
	return out
}
