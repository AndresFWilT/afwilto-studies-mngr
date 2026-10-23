package http

import (
	"net/http"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

func required(field string) domain.Violation {
	return domain.Violation{Field: field, Rule: "required", Message: field + " is required"}
}

func notNull(field string) domain.Violation {
	return domain.Violation{Field: field, Rule: "required", Message: field + " must not be null"}
}

func handleListSubjectExams(list in.ListSubjectExams) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		summaries, err := list.Execute(r.Context(), subjectID)
		if err != nil {
			translateError(w, err)
			return
		}
		dtos := make([]examSummaryDTO, len(summaries))
		for i, s := range summaries {
			dtos[i] = toExamSummaryDTO(s)
		}
		httpx.WriteList(w, http.StatusOK, dtos, listMeta{TotalItems: len(dtos)})
	}
}

func handleCreateExam(create in.CreateExam) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		subjectID, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		var req createExamRequest
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		var missing []domain.Violation
		for _, f := range []struct {
			field   string
			present bool
		}{
			{"name", req.Name != nil},
			{"questionCount", req.QuestionCount != nil},
			{"timeLimitMin", req.TimeLimitMin != nil},
			{"passPercent", req.PassPercent != nil},
		} {
			if !f.present {
				missing = append(missing, required(f.field))
			}
		}
		if len(missing) > 0 {
			translateError(w, domain.NewValidation(missing...))
			return
		}
		summary, err := create.Execute(r.Context(), in.CreateExamCommand{
			SubjectID: subjectID, Name: *req.Name,
			QuestionCount: *req.QuestionCount, TimeLimitMin: *req.TimeLimitMin, PassPercent: *req.PassPercent,
		})
		if err != nil {
			translateError(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/exams/"+summary.ID)
		httpx.WriteData(w, http.StatusCreated, toExamDTO(summary))
	}
}

func handleGetExam(get in.GetExam) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		summary, err := get.Execute(r.Context(), id)
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteData(w, http.StatusOK, toExamDTO(summary))
	}
}

func handleUpdateExam(update in.UpdateExam) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		var req updateExamRequest
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		var invalid []domain.Violation
		if req.Version == nil {
			invalid = append(invalid, required("version"))
		}
		for _, f := range []struct {
			field string
			null  bool
		}{
			{"name", req.Name.null()},
			{"questionCount", req.QuestionCount.null()},
			{"timeLimitMin", req.TimeLimitMin.null()},
			{"passPercent", req.PassPercent.null()},
		} {
			if f.null {
				invalid = append(invalid, notNull(f.field))
			}
		}
		if len(invalid) > 0 {
			translateError(w, domain.NewValidation(invalid...))
			return
		}
		summary, err := update.Execute(r.Context(), id, in.UpdateExamCommand{
			Version: *req.Version,
			Edit: domain.ExamEdit{
				Name: req.Name.Value, QuestionCount: req.QuestionCount.Value,
				TimeLimitMin: req.TimeLimitMin.Value, PassPercent: req.PassPercent.Value,
			},
		})
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteData(w, http.StatusOK, toExamDTO(summary))
	}
}

func handleDeleteExam(del in.DeleteExam) http.HandlerFunc {
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
