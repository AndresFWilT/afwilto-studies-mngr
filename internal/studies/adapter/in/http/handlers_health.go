package http

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

// Pinger reports whether the backing database answers. Satisfied by
// *pgxpool.Pool; faked in tests.
type Pinger interface {
	Ping(ctx context.Context) error
}

func handleHealthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteData(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleReadyz(pinger Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := pinger.Ping(r.Context()); err != nil {
			// A named component is reported by name; the cause (a path, a
			// connection error) is logged, never sent to the caller.
			message := "database unreachable"
			var notReady *httpx.NotReadyError
			if errors.As(err, &notReady) {
				message = notReady.Component + " unavailable"
			}
			slog.Error("readiness check failed", "err", err)
			httpx.WriteError(w, http.StatusServiceUnavailable, httpx.ErrorBody{
				Code:    "internal",
				Message: message,
			})
			return
		}
		httpx.WriteData(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
