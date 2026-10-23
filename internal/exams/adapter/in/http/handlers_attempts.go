package http

import (
	"net/http"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

func handlePreviewSession(preview in.PreviewSession) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, http.StatusNotImplemented, httpx.ErrorBody{Code: "internal", Message: "not implemented"})
	}
}

func handleStartAttempt(start in.StartAttempt) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteError(w, http.StatusNotImplemented, httpx.ErrorBody{Code: "internal", Message: "not implemented"})
	}
}
