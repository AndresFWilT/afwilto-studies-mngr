package http

import (
	"encoding/json"

	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// Opt distinguishes absent (Set=false) from explicit null (Set=true,
// Value=nil) so PATCH keeps merge-patch semantics.
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

func (o Opt[T]) patch() in.Patch[T] { return in.Patch[T]{Set: o.Set, Value: o.Value} }

type listMeta struct {
	TotalItems int `json:"totalItems"`
}

// ── Categories ──────────────────────────────────────────────────────────────

type createCategoryRequest struct {
	Name     string  `json:"name"`
	Color    string  `json:"color"`
	ParentID *string `json:"parentId"`
}

type updateCategoryRequest struct {
	Name     Opt[string] `json:"name"`
	Color    Opt[string] `json:"color"`
	ParentID Opt[string] `json:"parentId"`
}

type categoryDTO struct {
	ID           string  `json:"id"`
	ParentID     *string `json:"parentId"`
	Name         string  `json:"name"`
	Color        string  `json:"color"`
	Path         string  `json:"path"`
	Depth        int     `json:"depth"`
	SubjectCount int     `json:"subjectCount"`
}

func toCategoryDTO(v domain.CategoryView) categoryDTO {
	return categoryDTO{
		ID:           v.ID,
		ParentID:     v.ParentID,
		Name:         v.Name.String(),
		Color:        v.Color.String(),
		Path:         v.Path,
		Depth:        v.Depth,
		SubjectCount: v.SubjectCount,
	}
}
