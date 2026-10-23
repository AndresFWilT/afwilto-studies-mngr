package http

import (
	"net/http"
	"net/url"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

// parseQuestionFilter reads ?category=&origin=&difficulty=&q=&unclassified=.
// An empty category, origin, difficulty or q is no filter; unclassified is
// true or false.
func parseQuestionFilter(query url.Values) (domain.QuestionFilter, []domain.Violation) {
	f := domain.QuestionFilter{
		Origin: query.Get("origin"), Difficulty: query.Get("difficulty"), Text: query.Get("q"),
	}
	var bad []domain.Violation
	if category := query.Get("category"); category != "" {
		if httpx.IsUUID(category) {
			f.CategoryID = lowerID(category)
		} else {
			bad = append(bad, domain.Violation{Field: "category", Rule: "format", Message: "category must be a category id"})
		}
	}
	if query.Has("unclassified") {
		switch query.Get("unclassified") {
		case "true":
			f.Unclassified = true
		case "false":
		default:
			bad = append(bad, domain.Violation{Field: "unclassified", Rule: "format", Message: "unclassified must be true or false"})
		}
	}
	return f, append(bad, f.Violations()...)
}

func handleListExamQuestions(list in.ListExamQuestions) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		examID, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		filter, bad := parseQuestionFilter(r.URL.Query())
		if len(bad) > 0 {
			translateError(w, domain.NewValidation(bad...))
			return
		}
		summaries, err := list.Execute(r.Context(), examID, filter)
		if err != nil {
			translateError(w, err)
			return
		}
		dtos := make([]questionSummaryDTO, len(summaries))
		for i, s := range summaries {
			dtos[i] = toQuestionSummaryDTO(s)
		}
		httpx.WriteList(w, http.StatusOK, dtos, listMeta{TotalItems: len(dtos)})
	}
}

func handleGetQuestion(get in.GetQuestion) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		question, err := get.Execute(r.Context(), id)
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteData(w, http.StatusOK, toQuestionDTO(question))
	}
}

func handleDeleteQuestion(del in.DeleteQuestion) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		if err := del.Execute(r.Context(), id); err != nil {
			translateError(w, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleMergeCategories(merge in.MergeCategories) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		examID, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		var req mergeCategoriesRequest
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		var bad []domain.Violation
		for _, f := range []struct {
			field string
			value *string
		}{{"from", req.From}, {"into", req.Into}} {
			switch {
			case f.value == nil:
				bad = append(bad, required(f.field))
			case !httpx.IsUUID(*f.value):
				bad = append(bad, domain.Violation{Field: f.field, Rule: "format", Message: f.field + " must be a category id"})
			}
		}
		if len(bad) > 0 {
			translateError(w, domain.NewValidation(bad...))
			return
		}
		summary, err := merge.Execute(r.Context(), examID, in.MergeCategoriesCommand{From: lowerID(*req.From), Into: lowerID(*req.Into)})
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteData(w, http.StatusOK, toExamDTO(summary))
	}
}
