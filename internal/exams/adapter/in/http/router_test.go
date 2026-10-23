package http

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

func TestRegisterAddsNoRoutesYet(t *testing.T) { // slice 01 §1.3: an empty registrar
	mux := http.NewServeMux()
	Register(Deps{})(mux)

	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/subjects/0192f3a4-0000-7000-8000-000000000001/exams"},
		{http.MethodGet, "/api/v1/exams/0192f3a4-0000-7000-8000-000000000001"},
		{http.MethodPost, "/api/v1/exams/0192f3a4-0000-7000-8000-000000000001/questions/import"},
	} {
		if _, pattern := mux.Handler(httptest.NewRequest(tc.method, tc.path, nil)); pattern != "" {
			t.Errorf("%s %s is registered as %q; slice 01 registers nothing", tc.method, tc.path, pattern)
		}
	}
}

func TestRegisterComposesWithTheSharedServer(t *testing.T) { // slice 01 §1.3
	handler := httpx.NewServer(httpx.Config{}, Register(Deps{}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/exams/0192f3a4-0000-7000-8000-000000000001", nil))

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
