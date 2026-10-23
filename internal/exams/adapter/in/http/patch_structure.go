package http

import (
	"fmt"
	"strings"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// The nested items of PATCH /exams/{id} (contract UpdateExamRequest).

type classificationEditDTO struct {
	ID   *string     `json:"id"`
	Name Opt[string] `json:"name"`
}

type weightDTO struct {
	CategoryID *string `json:"categoryId"`
	Percent    *int    `json:"percent"`
}

type categoryEditDTO struct {
	ID            *string     `json:"id"`
	Name          Opt[string] `json:"name"`
	PaceQuestions Opt[int]    `json:"paceQuestions"`
	PaceMinutes   Opt[int]    `json:"paceMinutes"`
}

// lowerID puts an id in the canonical lower-case form the database stores.
func lowerID(id string) string { return strings.ToLower(id) }

// structure maps the slice 04 fields of the request to a domain.StructureEdit
// and reports the shape problems: a null where a value is required, an item
// without its id, a weightMode other than "inferred". Business rules are the
// domain's.
func (req updateExamRequest) structure() (domain.StructureEdit, []domain.Violation) {
	var edit domain.StructureEdit
	var bad []domain.Violation

	if req.Classifications.null() {
		bad = append(bad, notNull("classifications"))
	} else if req.Classifications.Value != nil {
		for i, c := range *req.Classifications.Value {
			path := fmt.Sprintf("classifications[%d]", i)
			if c.ID == nil {
				bad = append(bad, required(path+".id"))
				continue
			}
			if c.Name.null() {
				bad = append(bad, notNull(path+".name"))
				continue
			}
			edit.Classifications = append(edit.Classifications, domain.ClassificationRename{ID: lowerID(*c.ID), Name: c.Name.Value})
		}
	}

	switch {
	case req.PrimaryClassificationID.null():
		bad = append(bad, notNull("primaryClassificationId"))
	case req.PrimaryClassificationID.Value != nil && strings.TrimSpace(*req.PrimaryClassificationID.Value) == "":
		bad = append(bad, required("primaryClassificationId"))
	case req.PrimaryClassificationID.Value != nil:
		edit.PrimaryID = lowerID(*req.PrimaryClassificationID.Value)
	}

	if req.Weights.null() {
		bad = append(bad, notNull("weights"))
	} else if req.Weights.Value != nil {
		edit.SetWeights = true
		for i, wt := range *req.Weights.Value {
			path := fmt.Sprintf("weights[%d]", i)
			switch {
			case wt.CategoryID == nil:
				bad = append(bad, required(path+".categoryId"))
			case wt.Percent == nil:
				bad = append(bad, required(path+".percent"))
			default:
				edit.Weights = append(edit.Weights, domain.WeightInput{CategoryID: lowerID(*wt.CategoryID), Percent: *wt.Percent})
			}
		}
	}

	switch {
	case req.WeightMode.null():
		bad = append(bad, notNull("weightMode"))
	case req.WeightMode.Value != nil && *req.WeightMode.Value != string(domain.WeightInferred):
		bad = append(bad, domain.Violation{Field: "weightMode", Rule: "format", Message: `weightMode only accepts "inferred"; send weights to switch to manual`})
	case req.WeightMode.Value != nil:
		edit.InferWeights = true
	}

	if req.Categories.null() {
		bad = append(bad, notNull("categories"))
	} else if req.Categories.Value != nil {
		for i, c := range *req.Categories.Value {
			path := fmt.Sprintf("categories[%d]", i)
			if c.ID == nil {
				bad = append(bad, required(path+".id"))
				continue
			}
			if c.Name.null() {
				bad = append(bad, notNull(path+".name"))
				continue
			}
			edit.Categories = append(edit.Categories, domain.CategoryEdit{
				ID: lowerID(*c.ID), Name: c.Name.Value,
				Pace: domain.PaceEdit{
					QuestionsSet: c.PaceQuestions.Set, Questions: c.PaceQuestions.Value,
					MinutesSet: c.PaceMinutes.Set, Minutes: c.PaceMinutes.Value,
				},
			})
		}
	}
	return edit, bad
}
