package http

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

func TestRegisterAddsTheExamsCrudRoutesOnly(t *testing.T) { // slice 02: five routes, the rest still unregistered
	mux := http.NewServeMux()
	Register(Deps{})(mux)
	const examID = "0192f3a4-0000-7000-8000-000000000001"

	for _, tc := range []struct{ method, path, want string }{
		{http.MethodGet, "/api/v1/subjects/" + examID + "/exams", "GET /api/v1/subjects/{id}/exams"},
		{http.MethodPost, "/api/v1/subjects/" + examID + "/exams", "POST /api/v1/subjects/{id}/exams"},
		{http.MethodGet, "/api/v1/exams/" + examID, "GET /api/v1/exams/{id}"},
		{http.MethodPatch, "/api/v1/exams/" + examID, "PATCH /api/v1/exams/{id}"},
		{http.MethodDelete, "/api/v1/exams/" + examID, "DELETE /api/v1/exams/{id}"},
		// later slices
		{http.MethodPost, "/api/v1/exams/" + examID + "/questions/import", ""},
		{http.MethodGet, "/api/v1/exams/" + examID + "/attempts", ""},
	} {
		if _, pattern := mux.Handler(httptest.NewRequest(tc.method, tc.path, nil)); pattern != tc.want {
			t.Errorf("%s %s is registered as %q, want %q", tc.method, tc.path, pattern, tc.want)
		}
	}
}

func TestRegisterComposesWithTheSharedServer(t *testing.T) { // slice 01 §1.3
	handler := httpx.NewServer(httpx.Config{}, Register(Deps{}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/exams/0192f3a4-0000-7000-8000-000000000001/attempts", nil))

	if rec.Code != http.StatusNotFound || rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("status %d, Content-Type %q; want the JSON 404 fallback", rec.Code, rec.Header().Get("Content-Type"))
	}
}

func TestImportPatternIsTheDocumentedImportRoute(t *testing.T) { // S01-5 wiring
	if ImportPattern != "POST /api/v1/exams/{id}/questions/import" {
		t.Errorf("ImportPattern = %q", ImportPattern)
	}
	_, thisFile, _, _ := runtime.Caller(0)
	spec := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "api", "openapi.yaml")
	doc, err := openapi3.NewLoader().LoadFromFile(spec)
	if err != nil {
		t.Fatal(err)
	}
	item := doc.Paths.Find("/api/v1/exams/{id}/questions/import")
	if item == nil || item.Post == nil {
		t.Error("the contract does not document POST /api/v1/exams/{id}/questions/import")
	}
}

func TestBodyLimitsOverridesOnlyTheImportRoute(t *testing.T) { // S01-5 wiring
	got := BodyLimits(53477376)

	if len(got) != 1 || got[ImportPattern] != 53477376 {
		t.Errorf("BodyLimits = %v, want only {%q: 53477376}", got, ImportPattern)
	}
}

func TestRegisterAddsTheBankRoutesOnlyWhenTheirUseCasesAreWired(t *testing.T) { // slice 04
	const id = "0192f3a4-0000-7000-8000-000000000001"
	routes := []struct{ method, path, want string }{
		{http.MethodGet, "/api/v1/exams/" + id + "/questions", "GET /api/v1/exams/{id}/questions"},
		{http.MethodGet, "/api/v1/questions/" + id, "GET /api/v1/questions/{id}"},
		{http.MethodDelete, "/api/v1/questions/" + id, "DELETE /api/v1/questions/{id}"},
		{http.MethodPost, "/api/v1/exams/" + id + "/categories/merge", "POST /api/v1/exams/{id}/categories/merge"},
	}
	unwired, wired := http.NewServeMux(), http.NewServeMux()
	Register(Deps{})(unwired)
	Register(newBankDeps(t))(wired)

	for _, tc := range routes {
		if _, pattern := unwired.Handler(httptest.NewRequest(tc.method, tc.path, nil)); pattern != "" {
			t.Errorf("%s %s is registered without its use case: %q", tc.method, tc.path, pattern)
		}
		if _, pattern := wired.Handler(httptest.NewRequest(tc.method, tc.path, nil)); pattern != tc.want {
			t.Errorf("%s %s is registered as %q, want %q", tc.method, tc.path, pattern, tc.want)
		}
	}
}

func TestRegisterAddsTheSessionRoutesOnlyWhenTheirUseCasesAreWired(t *testing.T) { // slice 06a
	const id = "0192f3a4-0000-7000-8000-000000000001"
	routes := []struct{ method, path, want string }{
		{http.MethodPost, "/api/v1/exams/" + id + "/sessions/preview", "POST /api/v1/exams/{id}/sessions/preview"},
		{http.MethodPost, "/api/v1/exams/" + id + "/attempts", "POST /api/v1/exams/{id}/attempts"},
	}
	unwired, wired := http.NewServeMux(), http.NewServeMux()
	Register(Deps{})(unwired)
	Register(Deps{PreviewSession: stubPreview{}, StartAttempt: stubStart{}})(wired)

	for _, tc := range routes {
		if _, pattern := unwired.Handler(httptest.NewRequest(tc.method, tc.path, nil)); pattern != "" {
			t.Errorf("%s %s is registered without its use case: %q", tc.method, tc.path, pattern)
		}
		if _, pattern := wired.Handler(httptest.NewRequest(tc.method, tc.path, nil)); pattern != tc.want {
			t.Errorf("%s %s is registered as %q, want %q", tc.method, tc.path, pattern, tc.want)
		}
	}
	// the history stays unregistered until slice 06b
	if _, pattern := wired.Handler(httptest.NewRequest(http.MethodGet, "/api/v1/exams/"+id+"/attempts", nil)); pattern != "" {
		t.Errorf("GET /exams/{id}/attempts is registered as %q before slice 06b", pattern)
	}
}

type stubPreview struct{ in.PreviewSession }

type stubStart struct{ in.StartAttempt }
