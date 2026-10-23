package http

import (
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
)

// handleGetMedia serves a stored image by content hash (FR-6). The URL names
// the bytes, so they can be cached forever. A malformed or unknown hash is the
// plain JSON 404, decided by the use case.
func handleGetMedia(get in.GetMedia) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		content, err := get.Execute(r.Context(), r.PathValue("sha256"))
		if err != nil {
			translateError(w, err)
			return
		}
		defer content.Body.Close()
		h := w.Header()
		h.Set("Content-Type", content.Media.ContentType)
		h.Set("Content-Length", strconv.Itoa(content.Media.Bytes))
		h.Set("Cache-Control", "public, max-age=31536000, immutable")
		h.Set("X-Content-Type-Options", "nosniff")
		w.WriteHeader(http.StatusOK)
		if _, err := io.Copy(w, content.Body); err != nil {
			slog.Error("streaming media", "sha256", content.Media.SHA256, "err", err)
		}
	}
}
