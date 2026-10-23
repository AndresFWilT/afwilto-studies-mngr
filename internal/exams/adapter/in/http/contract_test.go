package http

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
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

func loadContract(t *testing.T) routers.Router {
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
	return contractRouter
}

// assertConformsToContract validates a recorded response against the OpenAPI
// schema for its route; undocumented status codes count as violations.
func assertConformsToContract(t *testing.T, method, path string, rec *httptest.ResponseRecorder) {
	t.Helper()
	req, _ := http.NewRequest(method, "http://localhost:8084"+path, nil)
	route, pathParams, err := loadContract(t).FindRoute(req)
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
	if err := openapi3filter.ValidateResponse(context.Background(), input); err != nil {
		t.Errorf("%s %s (%d) violates the contract: %v\nbody: %s", method, path, rec.Code, err, rec.Body.String())
	}
}
