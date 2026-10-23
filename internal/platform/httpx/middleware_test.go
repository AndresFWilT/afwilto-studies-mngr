package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPanicBecomesInternalError(t *testing.T) {
	handler := Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	}))
	rec := serve(handler, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"code":"internal"`) {
		t.Errorf("body = %s, want error envelope with code internal", rec.Body.String())
	}
}
