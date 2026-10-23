package http

import (
	"context"
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
			httpx.WriteError(w, http.StatusServiceUnavailable, httpx.ErrorBody{
				Code:    "internal",
				Message: "database unreachable",
			})
			return
		}
		httpx.WriteData(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
