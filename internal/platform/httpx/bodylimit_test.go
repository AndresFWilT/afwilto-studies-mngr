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

// Spec 18 slice 01 (S01-5): the import route gets its own, larger cap.
const importPattern = "POST /api/v1/exams/{id}/questions/import"

const (
	testDefaultLimit = 1 << 20 // 1 MiB, as MAX_BODY_BYTES
	testImportLimit  = 4 << 20 // stands in for MAX_IMPORT_BYTES
	twoMiB           = 2 << 20
)

const importPath = "/api/v1/exams/0192f3a4-0000-7000-8000-000000000001/questions/import"

// limitedServer registers a JSON-decoding handler on a studies route and a
// raw-reading handler on the import pattern, behind the real chain.
func limitedServer(onImport func(r *http.Request)) http.Handler {
	return NewServer(Config{
		MaxBodyBytes: testDefaultLimit,
		BodyLimits:   map[string]int64{importPattern: testImportLimit},
	}, func(mux *http.ServeMux) {
		mux.HandleFunc("POST /api/v1/subjects", func(w http.ResponseWriter, r *http.Request) {
			var dst map[string]any
			if DecodeJSON(w, r, &dst) {
				WriteData(w, http.StatusCreated, dst)
			}
		})
		mux.HandleFunc(importPattern, func(w http.ResponseWriter, r *http.Request) {
			onImport(r)
			w.WriteHeader(http.StatusNoContent)
		})
	})
}

func TestDefaultLimitStillRejects2MiBOnOrdinaryRoutes(t *testing.T) { // S01-5
	body := `{"name":"` + strings.Repeat("a", twoMiB) + `"}`
	for _, streamed := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/subjects", strings.NewReader(body))
		if streamed {
			req.ContentLength = -1
		}
		rec := serve(limitedServer(func(*http.Request) {}), req)

		if rec.Code != http.StatusRequestEntityTooLarge || !strings.Contains(rec.Body.String(), `"code":"payload_too_large"`) {
			t.Errorf("streamed=%v: status %d, body %s; want 413 payload_too_large", streamed, rec.Code, rec.Body.String())
		}
	}
}

func TestImportRouteReads2MiBInFull(t *testing.T) { // S01-5
	var read int
	var readErr error
	handler := limitedServer(func(r *http.Request) {
		var b []byte
		b, readErr = io.ReadAll(r.Body)
		read = len(b)
	})

	for _, streamed := range []bool{false, true} {
		read, readErr = 0, nil
		req := httptest.NewRequest(http.MethodPost, importPath, strings.NewReader(strings.Repeat("x", twoMiB)))
		if streamed {
			req.ContentLength = -1
		}
		rec := serve(handler, req)

		if rec.Code != http.StatusNoContent || readErr != nil || read != twoMiB {
			t.Errorf("streamed=%v: status %d, read %d bytes, err %v; want 204 and %d bytes", streamed, rec.Code, read, readErr, twoMiB)
		}
	}
}

func TestImportRouteRejectsOneByteOverItsLimit(t *testing.T) { // S01-5
	var readErr error
	handler := limitedServer(func(r *http.Request) { _, readErr = io.ReadAll(r.Body) })

	req := httptest.NewRequest(http.MethodPost, importPath, strings.NewReader(strings.Repeat("x", testImportLimit+1)))
	req.ContentLength = -1
	serve(handler, req)

	var tooLarge *http.MaxBytesError
	if !errors.As(readErr, &tooLarge) {
		t.Fatalf("read error = %v, want *http.MaxBytesError at MAX_IMPORT_BYTES+1", readErr)
	}
	if tooLarge.Limit != testImportLimit {
		t.Errorf("limit = %d, want %d", tooLarge.Limit, testImportLimit)
	}
}

func TestImportRouteAcceptsExactlyItsLimit(t *testing.T) { // S01-5 boundary
	var read int
	var readErr error
	handler := limitedServer(func(r *http.Request) {
		b, err := io.ReadAll(r.Body)
		read, readErr = len(b), err
	})

	req := httptest.NewRequest(http.MethodPost, importPath, strings.NewReader(strings.Repeat("x", testImportLimit)))
	req.ContentLength = -1
	serve(handler, req)

	if readErr != nil || read != testImportLimit {
		t.Errorf("read %d bytes, err %v; want %d and nil", read, readErr, testImportLimit)
	}
}

func TestImportRouteRejectsDeclaredOversizeUpFront(t *testing.T) { // S01-5
	reached := false
	handler := limitedServer(func(*http.Request) { reached = true })

	req := httptest.NewRequest(http.MethodPost, importPath, strings.NewReader(strings.Repeat("x", testImportLimit+1)))
	rec := serve(handler, req)

	if rec.Code != http.StatusRequestEntityTooLarge || reached {
		t.Errorf("status %d, handler reached %v; want 413 without reaching the handler", rec.Code, reached)
	}
}

func TestOverrideDoesNotLeakToOtherMethodsOnTheSamePath(t *testing.T) { // S01-5
	reached := false
	handler := NewServer(Config{
		MaxBodyBytes: testDefaultLimit,
		BodyLimits:   map[string]int64{importPattern: testImportLimit},
	}, func(mux *http.ServeMux) {
		mux.HandleFunc(importPattern, func(http.ResponseWriter, *http.Request) {})
		mux.HandleFunc("PUT /api/v1/exams/{id}/questions/import", func(w http.ResponseWriter, r *http.Request) {
			reached = true
			w.WriteHeader(http.StatusNoContent)
		})
	})

	rec := serve(handler, httptest.NewRequest(http.MethodPut, importPath, strings.NewReader(strings.Repeat("x", twoMiB))))

	if rec.Code != http.StatusRequestEntityTooLarge || reached {
		t.Errorf("PUT with 2 MiB: status %d, reached %v; want 413 from the default limit", rec.Code, reached)
	}
}
