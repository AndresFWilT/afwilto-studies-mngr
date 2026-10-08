package http

import (
	"context"
	"net/http"
)

// Pinger reports whether the backing database answers. Satisfied by
// *pgxpool.Pool; faked in tests.
type Pinger interface {
	Ping(ctx context.Context) error
}

func handleHealthz() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeData(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func handleReadyz(pinger Pinger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := pinger.Ping(r.Context()); err != nil {
			writeError(w, http.StatusServiceUnavailable, errorBody{
				Code:    "internal",
				Message: "database unreachable",
			})
			return
		}
		writeData(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
