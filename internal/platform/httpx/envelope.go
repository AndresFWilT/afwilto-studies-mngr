package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

// ErrorBody is the payload under "error" in every failure response.
type ErrorBody struct {
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Details []ErrorDetail `json:"details,omitempty"`
}

// ErrorDetail pinpoints one violated rule on one field.
type ErrorDetail struct {
	Field   string `json:"field"`
	Rule    string `json:"rule"`
	Message string `json:"message"`

	// Spec 18 insufficient_questions only: the category that is short and its
	// numbers. Available can be 0, so the numbers are pointers.
	Category  string `json:"category,omitempty"`
	Needed    *int   `json:"needed,omitempty"`
	Available *int   `json:"available,omitempty"`
}

func WriteData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data}); err != nil {
		slog.Error("encoding response", "err", err)
	}
}

func WriteList(w http.ResponseWriter, status int, data any, meta any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{"data": data, "meta": meta}); err != nil {
		slog.Error("encoding response", "err", err)
	}
}

func WriteError(w http.ResponseWriter, status int, body ErrorBody) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{"error": body}); err != nil {
		slog.Error("encoding error response", "err", err)
	}
}

// NotFound answers the 404 envelope used for unknown and deleted resources.
func NotFound(w http.ResponseWriter) {
	WriteError(w, http.StatusNotFound, ErrorBody{Code: "not_found", Message: "resource not found"})
}
