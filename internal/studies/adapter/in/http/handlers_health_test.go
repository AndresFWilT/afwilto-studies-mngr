package http

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type stubPinger struct{ err error }

func (s stubPinger) Ping(context.Context) error { return s.err }

func newTestRouter(pingErr error) http.Handler {
	return NewRouter(RouterDeps{
		Pinger:       stubPinger{err: pingErr},
		AllowOrigins: []string{"http://localhost:3000"},
	})
}

func serve(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestHealthzAlwaysOK(t *testing.T) {
	rec := serve(newTestRouter(errors.New("db down")), httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"data":{"status":"ok"}}` {
		t.Errorf("body = %s, want {\"data\":{\"status\":\"ok\"}}", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestReadyzOKWhenDBAnswers(t *testing.T) {
	rec := serve(newTestRouter(nil), httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestReadyzUnavailableWhenDBDown(t *testing.T) {
	rec := serve(newTestRouter(errors.New("connection refused")), httptest.NewRequest(http.MethodGet, "/readyz", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"internal"`) {
		t.Errorf("body = %s, want error envelope with code internal", rec.Body.String())
	}
}

func TestCORSEchoesConfiguredOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := serve(newTestRouter(nil), req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Errorf("Allow-Origin = %q, want the configured origin", got)
	}
	if got := rec.Header().Get("Vary"); got != "Origin" {
		t.Errorf("Vary = %q, want Origin", got)
	}
}

func TestCORSIgnoresUnknownOrigin(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Origin", "http://evil.test")
	rec := serve(newTestRouter(nil), req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("Allow-Origin = %q, want empty for unknown origin", got)
	}
}

func TestPreflightNoContent(t *testing.T) {
	req := httptest.NewRequest(http.MethodOptions, "/api/v1/notes/x", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "PATCH")
	rec := serve(newTestRouter(nil), req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, "PATCH") {
		t.Errorf("Allow-Methods = %q, want PATCH included", got)
	}
}

func TestUnknownPathIsJSONNotFound(t *testing.T) {
	rec := serve(newTestRouter(nil), httptest.NewRequest(http.MethodGet, "/nope", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"not_found"`) {
		t.Errorf("body = %s, want JSON error envelope with code not_found", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

func TestWrongMethodIsJSONMethodNotAllowed(t *testing.T) {
	rec := serve(newTestRouter(nil), httptest.NewRequest(http.MethodPost, "/healthz", nil))

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"method_not_allowed"`) {
		t.Errorf("body = %s, want JSON error envelope with code method_not_allowed", rec.Body.String())
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "GET") {
		t.Errorf("Allow = %q, want GET listed", allow)
	}
}
