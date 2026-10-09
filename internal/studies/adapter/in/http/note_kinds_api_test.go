package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const kindsPath = "/api/v1/note-kinds"

func (e apiEnv) createKind(t *testing.T, name string) map[string]any {
	t.Helper()
	rec := e.do(t, http.MethodPost, kindsPath, fmt.Sprintf(`{"name":%q}`, name))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create kind %q: status %d, body %s", name, rec.Code, rec.Body.String())
	}
	return decodeData(t, rec)
}

func TestCreateNoteKindAPI(t *testing.T) {
	e := newAPIEnv()
	rec := e.do(t, http.MethodPost, kindsPath, `{"name":"  Lab report "}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	data := decodeData(t, rec)
	if data["name"] != "Lab report" || data["noteCount"] != float64(0) {
		t.Errorf("data = %v", data)
	}
	if loc := rec.Header().Get("Location"); loc != kindsPath+"/"+data["id"].(string) {
		t.Errorf("Location = %q", loc)
	}
}

func TestNoteKindWriteErrorsAPI(t *testing.T) {
	e := newAPIEnv()
	e.createKind(t, "Lab report")
	tests := []struct {
		name, body string
		status     int
		code       string
		field      string
		rule       string
	}{
		{"duplicate differs by case", `{"name":"lab report"}`, 409, "conflict", "name", "duplicate_name"},
		{"missing name", `{}`, 422, "validation_failed", "name", "required"},
		{"blank name", `{"name":"  "}`, 422, "validation_failed", "name", "required"},
		{"41 chars", `{"name":"` + strings.Repeat("a", 41) + `"}`, 422, "validation_failed", "name", "max_length"},
		{"control char", `{"name":"a\u0000b"}`, 422, "validation_failed", "name", "format"},
		{"invalid json", `{"name":`, 422, "validation_failed", "body", "json"},
		{"trailing data", `{"name":"X"} {}`, 422, "validation_failed", "body", "json"},
		{"too large", `{"name":"` + strings.Repeat("a", testMaxBodyBytes) + `"}`, 413, "payload_too_large", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantError(t, e.do(t, http.MethodPost, kindsPath, tc.body), tc.status, tc.code, tc.field, tc.rule)
		})
	}
}

func TestListNoteKindsAPI(t *testing.T) {
	e := newAPIEnv()
	for _, n := range []string{"Summary", "cheat sheet", "Notes"} {
		e.createKind(t, n)
	}
	rec := e.do(t, http.MethodGet, kindsPath, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var env struct {
		Data []map[string]any `json:"data"`
		Meta map[string]any   `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, d := range env.Data {
		names = append(names, d["name"].(string))
	}
	if got := strings.Join(names, ","); got != "cheat sheet,Notes,Summary" {
		t.Errorf("names = %s", got)
	}
	if env.Meta["totalItems"] != float64(3) {
		t.Errorf("meta = %v", env.Meta)
	}
}

func TestGetNoteKindAPI(t *testing.T) {
	e := newAPIEnv()
	k := e.createKind(t, "Notes")
	id := k["id"].(string)

	if rec := e.do(t, http.MethodGet, kindsPath+"/"+strings.ToUpper(id), ""); rec.Code != 200 || decodeData(t, rec)["id"] != id {
		t.Errorf("upper-case id: status %d, body %s", rec.Code, rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodGet, kindsPath+"/"+unknownID, ""), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodGet, kindsPath+"/not-a-uuid", ""), 404, "not_found", "", "")
}

func TestRenameNoteKindAPI(t *testing.T) {
	e := newAPIEnv()
	e.createKind(t, "Summary")
	notes := e.createKind(t, "Notes")
	path := kindsPath + "/" + notes["id"].(string)

	rec := e.do(t, http.MethodPatch, path, `{"name":"Lab"}`)
	if rec.Code != 200 || decodeData(t, rec)["name"] != "Lab" {
		t.Fatalf("rename: status %d, body %s", rec.Code, rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodPatch, path, `{"name":"summary"}`), 409, "conflict", "name", "duplicate_name")
	wantError(t, e.do(t, http.MethodPatch, path, `{}`), 422, "validation_failed", "name", "required")
	wantError(t, e.do(t, http.MethodPatch, path, `{"name":null}`), 422, "validation_failed", "name", "required")
	wantError(t, e.do(t, http.MethodPatch, path, `{"name":"`+strings.Repeat("a", testMaxBodyBytes)+`"}`), 413, "payload_too_large", "", "")
	wantError(t, e.do(t, http.MethodPatch, kindsPath+"/"+unknownID, `{"name":"X"}`), 404, "not_found", "", "")
}

func TestDeleteNoteKindAPI(t *testing.T) {
	e := newAPIEnv()
	k := e.createKind(t, "Notes")
	path := kindsPath + "/" + k["id"].(string)

	if rec := e.do(t, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body %s", rec.Code, rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodGet, path, ""), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodPatch, path, `{"name":"X"}`), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodDelete, path, ""), 404, "not_found", "", "")
	e.createKind(t, "Notes") // name freed
}
