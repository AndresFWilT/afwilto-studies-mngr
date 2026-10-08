package http

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type errorBody struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Details []errorDetail `json:"details,omitempty"`
}

type errorDetail struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`
}

func writeData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
		slog.Error("encoding response", "err", err)
	}
}

func writeList(w http.ResponseWriter, status int, data any, meta any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data, "meta": meta}); err != nil {
		slog.Error("encoding response", "err", err)
	}
}

func writeError(w http.ResponseWriter, status int, body errorBody) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{"error": body}); err != nil {
		slog.Error("encoding error response", "err", err)
	}
}
