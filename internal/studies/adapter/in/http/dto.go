package http

import (
	"encoding/json"
	"time"

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

// ── Note kinds ──────────────────────────────────────────────────────────────

type noteKindRequest struct {
	Name string `json:"name"`
}

type noteKindDTO struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	NoteCount int    `json:"noteCount"`
}

func toNoteKindDTO(v domain.NoteKindView) noteKindDTO {
	return noteKindDTO{ID: v.ID, Name: v.Name.String(), NoteCount: v.NoteCount}
}

// ── Subjects ────────────────────────────────────────────────────────────────

type createSubjectRequest struct {
	CategoryID  *string `json:"categoryId"`
	Name        string  `json:"name"`
	Description *string `json:"description"`
}

type updateSubjectRequest struct {
	CategoryID  Opt[string] `json:"categoryId"`
	Name        Opt[string] `json:"name"`
	Description Opt[string] `json:"description"`
}

type categoryRefDTO struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Path  string `json:"path"`
}

type subjectDTO struct {
	ID          string         `json:"id"`
	CategoryID  string         `json:"categoryId"`
	Category    categoryRefDTO `json:"category"`
	Name        string         `json:"name"`
	Description *string        `json:"description"`
	NoteCount   int            `json:"noteCount"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
}

func toSubjectDTO(v domain.SubjectView) subjectDTO {
	return subjectDTO{
		ID:         v.ID,
		CategoryID: v.CategoryID,
		Category: categoryRefDTO{
			ID: v.Category.ID, Name: v.Category.Name, Color: v.Category.Color, Path: v.Category.Path,
		},
		Name:        v.Name.String(),
		Description: v.Description,
		NoteCount:   v.NoteCount,
		CreatedAt:   v.CreatedAt,
		UpdatedAt:   v.UpdatedAt,
	}
}

// ── Notes ───────────────────────────────────────────────────────────────────

type createNoteRequest struct {
	Title     string            `json:"title"`
	KindID    *string           `json:"kindId"`
	Content   *string           `json:"content"`
	Tags      []string          `json:"tags"`
	SlabLinks []json.RawMessage `json:"slabLinks"`
}

type updateNoteRequest struct {
	Version   *int              `json:"version"`
	Title     Opt[string]       `json:"title"`
	KindID    Opt[string]       `json:"kindId"`
	SubjectID Opt[string]       `json:"subjectId"`
	Content   Opt[string]       `json:"content"`
	Tags      []string          `json:"tags"`
	SlabLinks []json.RawMessage `json:"slabLinks"`
}

type noteKindRefDTO struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type noteSummaryDTO struct {
	ID        string         `json:"id"`
	SubjectID string         `json:"subjectId"`
	Title     string         `json:"title"`
	Kind      noteKindRefDTO `json:"kind"`
	Tags      []string       `json:"tags"`
	CreatedAt time.Time      `json:"createdAt"`
	UpdatedAt time.Time      `json:"updatedAt"`
	Version   int            `json:"version"`
}

// noteDTO is the full note; slabLinks arrive in Slice 06 and are always empty.
type noteDTO struct {
	noteSummaryDTO
	Content   string `json:"content"`
	SlabLinks []any  `json:"slabLinks"`
}

func toNoteSummaryDTO(v domain.NoteView) noteSummaryDTO {
	tags := v.Tags
	if tags == nil {
		tags = []string{}
	}
	return noteSummaryDTO{
		ID: v.ID, SubjectID: v.SubjectID, Title: v.Title.String(),
		Kind:      noteKindRefDTO{ID: v.Kind.ID, Name: v.Kind.Name},
		Tags:      tags,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt, Version: v.Version,
	}
}

func toNoteDTO(v domain.NoteView) noteDTO {
	return noteDTO{noteSummaryDTO: toNoteSummaryDTO(v), Content: v.Content.String(), SlabLinks: []any{}}
}
