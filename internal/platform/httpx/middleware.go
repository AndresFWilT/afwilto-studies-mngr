package httpx

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"slices"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// RequestLog logs one structured line per request.
func RequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request",
			"id", requestID(),
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

func requestID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b)
}

// Recover turns a panic into the 500 envelope.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("panic recovered", "panic", rec, "path", r.URL.Path)
				WriteError(w, http.StatusInternalServerError, ErrorBody{
					Code:    "internal",
					Message: "internal server error",
				})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// CORS allows the listed origins and answers preflights.
func CORS(allowOrigins []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && slices.Contains(allowOrigins, origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// BodyLimit caps every request body; reads past the cap fail with
// *http.MaxBytesError, which the JSON decoder maps to 413 payload_too_large.
func BodyLimit(maxBytes int64, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if carriesBody(r.Method) && r.ContentLength > maxBytes {
			WriteError(w, http.StatusRequestEntityTooLarge, ErrorBody{
				Code:    "payload_too_large",
				Message: "request body is too large",
			})
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
		next.ServeHTTP(w, r)
	})
}

// carriesBody limits the up-front 413 to the methods whose bodies the API
// reads — the contract documents 413 only for those.
func carriesBody(method string) bool {
	return method == http.MethodPost || method == http.MethodPatch || method == http.MethodPut
}

// JSONFallback keeps ServeMux's routing decisions but replaces its plain-text
// 404/405 bodies with the API's JSON error envelope (Spec 16 §7).
func JSONFallback(mux *http.ServeMux) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern != "" {
			mux.ServeHTTP(w, r)
			return
		}
		probe := &headerOnlyRecorder{header: http.Header{}, status: http.StatusNotFound}
		mux.ServeHTTP(probe, r)
		if probe.status == http.StatusMethodNotAllowed {
			w.Header().Set("Allow", probe.header.Get("Allow"))
			WriteError(w, http.StatusMethodNotAllowed, ErrorBody{
				Code:    "method_not_allowed",
				Message: "method " + r.Method + " is not allowed on " + r.URL.Path,
			})
			return
		}
		WriteError(w, http.StatusNotFound, ErrorBody{
			Code:    "not_found",
			Message: "no route for " + r.Method + " " + r.URL.Path,
		})
	})
}

// headerOnlyRecorder captures the mux's fallback status and headers while
// discarding its plain-text body.
type headerOnlyRecorder struct {
	header http.Header
	status int
}

func (h *headerOnlyRecorder) Header() http.Header         { return h.header }
func (h *headerOnlyRecorder) Write(b []byte) (int, error) { return len(b), nil }
func (h *headerOnlyRecorder) WriteHeader(code int)        { h.status = code }
