package http

import (
	"net/http"
	"strconv"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

func handlePreviewSession(preview in.PreviewSession) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		examID, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		var req previewSessionRequest
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		cfg, bad := req.Config.toDomain()
		if len(bad) > 0 {
			translateError(w, domain.NewValidation(bad...))
			return
		}
		result, err := preview.Execute(r.Context(), examID, cfg)
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteData(w, http.StatusOK, toSessionPreviewDTO(result))
	}
}

func handleStartAttempt(start in.StartAttempt) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		examID, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		var req startAttemptRequest
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		cfg, bad := req.Config.toDomain()
		seed, seedBad := parseSeed(req.Seed)
		if bad = append(bad, seedBad...); len(bad) > 0 {
			translateError(w, domain.NewValidation(bad...))
			return
		}
		view, err := start.Execute(r.Context(), examID, in.StartAttemptCommand{Config: cfg, Seed: seed})
		if err != nil {
			translateError(w, err)
			return
		}
		w.Header().Set("Location", "/api/v1/attempts/"+view.ID)
		httpx.WriteData(w, http.StatusCreated, toAttemptDTO(view))
	}
}

func handleGetAttempt(get in.GetAttempt) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		view, err := get.Execute(r.Context(), id)
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteData(w, http.StatusOK, toAttemptDTO(view))
	}
}

// pathPosition reads the {position} path value: a whole number from 0. Anything
// else can never name an item, so it answers 404 like a malformed id.
func pathPosition(w http.ResponseWriter, r *http.Request) (int, bool) {
	position, err := strconv.ParseUint(r.PathValue("position"), 10, 31)
	if err != nil {
		httpx.NotFound(w)
		return 0, false
	}
	return int(position), true
}

func handleAnswerItem(answer in.AnswerItem) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		position, ok := pathPosition(w, r)
		if !ok {
			return
		}
		var req answerRequest
		if !httpx.DecodeJSON(w, r, &req) {
			return
		}
		input, bad := req.toDomain()
		if len(bad) > 0 {
			translateError(w, domain.NewValidation(bad...))
			return
		}
		outcome, err := answer.Execute(r.Context(), id, position, input)
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteData(w, http.StatusOK, toAnswerDTO(outcome))
	}
}

func handleSubmitAttempt(submit in.SubmitAttempt) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		result, err := submit.Execute(r.Context(), id)
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteData(w, http.StatusOK, toResultDTO(result))
	}
}

func handleReviewAttempt(review in.ReviewAttempt) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		items, err := review.Execute(r.Context(), id)
		if err != nil {
			translateError(w, err)
			return
		}
		httpx.WriteList(w, http.StatusOK, toReviewItemDTOs(items), map[string]int{"totalItems": len(items)})
	}
}

func handleListExamAttempts(list in.ListExamAttempts) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		examID, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		history, err := list.Execute(r.Context(), examID)
		if err != nil {
			translateError(w, err)
			return
		}
		rows, meta := toHistoryDTO(history)
		httpx.WriteList(w, http.StatusOK, rows, meta)
	}
}
