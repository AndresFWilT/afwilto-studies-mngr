package http

import (
	"net/http"

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
