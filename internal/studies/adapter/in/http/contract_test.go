package http

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers"
	"github.com/getkin/kin-openapi/routers/gorillamux"
)

var (
	contractOnce   sync.Once
	contractDoc    *openapi3.T
	contractRouter routers.Router
	contractErr    error
)

func loadContract(t *testing.T) (*openapi3.T, routers.Router) {
	t.Helper()
	contractOnce.Do(func() {
		// kin-openapi ignores `format` unless a validator is registered.
		openapi3.DefineStringFormatValidator("uuid", openapi3.NewRegexpFormatValidator(
			`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`))
		_, thisFile, _, _ := runtime.Caller(0)
		specPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "..", "..", "..", "api", "openapi.yaml")
		loader := openapi3.NewLoader()
		contractDoc, contractErr = loader.LoadFromFile(specPath)
		if contractErr != nil {
			return
		}
		if contractErr = contractDoc.Validate(loader.Context); contractErr != nil {
			return
		}
		contractRouter, contractErr = gorillamux.NewRouter(contractDoc)
	})
	if contractErr != nil {
		t.Fatalf("loading openapi.yaml: %v", contractErr)
	}
	return contractDoc, contractRouter
}

// assertConformsToContract validates a recorded response against the OpenAPI
// schema for its route — the executable contract-drift guard.
func assertConformsToContract(t *testing.T, method, path string, rec *httptest.ResponseRecorder) {
	t.Helper()
	if err := contractViolation(t, method, path, rec); err != nil {
		t.Errorf("%s %s (%d) violates the contract: %v\nbody: %s", method, path, rec.Code, err, rec.Body.String())
	}
}

// contractViolation returns why a recorded response breaks the contract, or
// nil. Undocumented status codes count as violations (IncludeResponseStatus).
func contractViolation(t *testing.T, method, path string, rec *httptest.ResponseRecorder) error {
	t.Helper()
	_, router := loadContract(t)

	req, _ := http.NewRequest(method, "http://localhost:8084"+path, nil)
	route, pathParams, err := router.FindRoute(req)
	if err != nil {
		t.Fatalf("route %s %s not in contract: %v", method, path, err)
	}

	input := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: &openapi3filter.RequestValidationInput{
			Request:    req,
			PathParams: pathParams,
			Route:      route,
			Options:    &openapi3filter.Options{ExcludeRequestBody: true},
		},
		Status:  rec.Code,
		Header:  rec.Header(),
		Body:    io.NopCloser(bytes.NewReader(rec.Body.Bytes())),
		Options: &openapi3filter.Options{IncludeResponseStatus: true},
	}
	return openapi3filter.ValidateResponse(context.Background(), input)
}

// Every endpoint of Spec 16 §7 must stay documented from v0.1.0 on, so slices
// 02–06 implement against a fixed contract instead of growing it ad hoc.
func TestContractDocumentsEveryEndpoint(t *testing.T) {
	doc, _ := loadContract(t)

	want := []string{
		"GET /healthz",
		"GET /readyz",
		"GET /api/v1/categories",
		"POST /api/v1/categories",
		"GET /api/v1/categories/{id}",
		"PATCH /api/v1/categories/{id}",
		"DELETE /api/v1/categories/{id}",
		"GET /api/v1/note-kinds",
		"POST /api/v1/note-kinds",
		"GET /api/v1/note-kinds/{id}",
		"PATCH /api/v1/note-kinds/{id}",
		"DELETE /api/v1/note-kinds/{id}",
		"GET /api/v1/subjects",
		"POST /api/v1/subjects",
		"GET /api/v1/subjects/{id}",
		"PATCH /api/v1/subjects/{id}",
		"DELETE /api/v1/subjects/{id}",
		"GET /api/v1/subjects/{id}/notes",
		"POST /api/v1/subjects/{id}/notes",
		"GET /api/v1/notes",
		"GET /api/v1/notes/{id}",
		"PATCH /api/v1/notes/{id}",
		"DELETE /api/v1/notes/{id}",
		"GET /api/v1/tags",
	}
	for _, endpoint := range want {
		method, path, _ := strings.Cut(endpoint, " ")
		item := doc.Paths.Find(path)
		if item == nil || item.GetOperation(method) == nil {
			t.Errorf("contract is missing %s", endpoint)
		}
	}
	if got := doc.Info.Version; got != "0.2.0" {
		t.Errorf("info.version = %q, want 0.2.0", got)
	}
}

func TestHealthResponsesConformToContract(t *testing.T) {
	up := newTestRouter(nil)
	down := newTestRouter(errors.New("db down"))

	for _, tc := range []struct {
		name   string
		router http.Handler
		path   string
	}{
		{"healthz", up, "/healthz"},
		{"readyz up", up, "/readyz"},
		{"readyz down", down, "/readyz"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := serve(tc.router, httptest.NewRequest(http.MethodGet, tc.path, nil))
			assertConformsToContract(t, http.MethodGet, tc.path, rec)
		})
	}
}

// The helper must actually reject non-conforming responses; otherwise every
// later slice's conformance assertion is vacuous.
func TestContractCheckRejectsViolations(t *testing.T) {
	cases := []struct {
		name, method, path string
		status             int
		body               string
	}{
		{"undocumented status", http.MethodGet, "/readyz", http.StatusInternalServerError,
			`{"error":{"code":"internal","message":"boom"}}`},
		{"non-uuid id", http.MethodGet, "/api/v1/note-kinds/0192f3a4-0000-7000-8000-000000000001", http.StatusOK,
			`{"data":{"id":"not-a-uuid","name":"Summary","noteCount":0}}`},
		{"unknown error code", http.MethodGet, "/readyz", http.StatusServiceUnavailable,
			`{"error":{"code":"teapot","message":"x"}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			rec.Header().Set("Content-Type", "application/json")
			rec.WriteHeader(tc.status)
			rec.Body.WriteString(tc.body)
			if err := contractViolation(t, tc.method, tc.path, rec); err == nil {
				t.Errorf("contract accepted a non-conforming %s %s (%d) response", tc.method, tc.path, tc.status)
			}
		})
	}
}
