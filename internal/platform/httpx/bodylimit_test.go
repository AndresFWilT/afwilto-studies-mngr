package httpx

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestBodyLimitMiddlewareCapsReads(t *testing.T) {
	var (
		readErr error
		reached bool
	)
	handler := BodyLimit(4, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		_, readErr = io.ReadAll(r.Body)
	}))

	// declared too large: rejected before the handler runs
	rec := serve(handler, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("12345")))
	if rec.Code != http.StatusRequestEntityTooLarge || reached {
		t.Errorf("declared oversize: status %d, handler reached %v; want 413 and not reached", rec.Code, reached)
	}
	if !strings.Contains(rec.Body.String(), `"code":"payload_too_large"`) {
		t.Errorf("body = %s, want payload_too_large envelope", rec.Body.String())
	}

	// streamed (unknown length): reads past the cap fail
	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("12345"))
	req.ContentLength = -1
	serve(handler, req)
	var tooLarge *http.MaxBytesError
	if !errors.As(readErr, &tooLarge) {
		t.Fatalf("streamed read error = %v, want *http.MaxBytesError", readErr)
	}

	readErr = nil
	serve(handler, httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("1234")))
	if readErr != nil {
		t.Errorf("read error at exactly the limit = %v, want nil", readErr)
	}
}

func TestBodyLimitIgnoresDeclaredLengthOnBodylessMethods(t *testing.T) {
	reached := false
	handler := BodyLimit(4, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached = true
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		reached = false
		rec := serve(handler, httptest.NewRequest(method, "/x", strings.NewReader("12345")))
		if rec.Code == http.StatusRequestEntityTooLarge || !reached {
			t.Errorf("%s with a large body: status %d, handler reached %v; want the handler to run", method, rec.Code, reached)
		}
	}
}
