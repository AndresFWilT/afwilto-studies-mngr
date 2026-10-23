package httpx

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func okRegistrar(pattern string) func(*http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			WriteData(w, http.StatusOK, map[string]string{"route": pattern})
		})
	}
}

func TestNewServerRunsEveryRegistrar(t *testing.T) {
	handler := NewServer(Config{}, okRegistrar("GET /a"), okRegistrar("GET /b"))

	for _, path := range []string{"/a", "/b"} {
		rec := serve(handler, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s: status %d, want 200", path, rec.Code)
		}
	}
}

func TestNewServerWrapsRoutesInTheMiddlewareChain(t *testing.T) {
	handler := NewServer(Config{AllowOrigins: []string{"http://localhost:3000"}, MaxBodyBytes: 4},
		okRegistrar("POST /a"),
		func(mux *http.ServeMux) {
			mux.HandleFunc("GET /boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
		})

	cors := httptest.NewRequest(http.MethodOptions, "/a", nil)
	cors.Header.Set("Origin", "http://localhost:3000")
	if rec := serve(handler, cors); rec.Code != http.StatusNoContent || rec.Header().Get("Access-Control-Allow-Origin") == "" {
		t.Errorf("CORS preflight: status %d, headers %v; want 204 with Allow-Origin", rec.Code, rec.Header())
	}
	if rec := serve(handler, httptest.NewRequest(http.MethodPost, "/a", strings.NewReader("12345"))); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize body: status %d, want 413", rec.Code)
	}
	if rec := serve(handler, httptest.NewRequest(http.MethodGet, "/boom", nil)); rec.Code != http.StatusInternalServerError {
		t.Errorf("panic: status %d, want 500", rec.Code)
	}
}

func TestZeroMaxBodyBytesDisablesTheCap(t *testing.T) {
	handler := NewServer(Config{}, okRegistrar("POST /a"))

	rec := serve(handler, httptest.NewRequest(http.MethodPost, "/a", strings.NewReader(strings.Repeat("x", 1<<20+1))))
	if rec.Code != http.StatusOK {
		t.Errorf("status %d, want 200 without a cap", rec.Code)
	}
}

// S01-7: a route that is documented but not registered answers the JSON 404
// envelope, not the mux's plain-text body.
func TestUnregisteredRouteAnswersJSONNotFound(t *testing.T) {
	handler := NewServer(Config{MaxBodyBytes: testDefaultLimit}, okRegistrar("GET /api/v1/subjects"))

	rec := serve(handler, httptest.NewRequest(http.MethodGet, "/api/v1/subjects/0192f3a4-0000-7000-8000-000000000001/exams", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if body := rec.Body.String(); !strings.Contains(body, `"error":{"code":"not_found"`) {
		t.Errorf("body = %s, want the not_found error envelope", body)
	}
}

func TestWrongMethodOnRegisteredRouteAnswersJSON405(t *testing.T) {
	handler := NewServer(Config{}, okRegistrar("GET /a"))

	rec := serve(handler, httptest.NewRequest(http.MethodDelete, "/a", nil))

	if rec.Code != http.StatusMethodNotAllowed || !strings.Contains(rec.Body.String(), `"code":"method_not_allowed"`) {
		t.Errorf("status %d, body %s; want 405 method_not_allowed", rec.Code, rec.Body.String())
	}
}

// Slice 06b decision 4: the answer endpoint is a PUT, so the preflight must
// allow it, with every method the API uses.
func TestCORSPreflightAllowsPutAndTheOtherMethodsTheAPIUses(t *testing.T) {
	handler := NewServer(Config{AllowOrigins: []string{"http://localhost:3000"}}, okRegistrar("PUT /a"))
	req := httptest.NewRequest(http.MethodOptions, "/a", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "PUT")

	rec := serve(handler, req)

	allowed := strings.Split(strings.ReplaceAll(rec.Header().Get("Access-Control-Allow-Methods"), " ", ""), ",")
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
		if !slices.Contains(allowed, method) {
			t.Errorf("Access-Control-Allow-Methods = %q lacks %s", rec.Header().Get("Access-Control-Allow-Methods"), method)
		}
	}
}
