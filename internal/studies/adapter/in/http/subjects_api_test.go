package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const subjectsPath = "/api/v1/subjects"

func (e apiEnv) createSubject(t *testing.T, categoryID, name string) map[string]any {
	t.Helper()
	rec := e.do(t, http.MethodPost, subjectsPath, fmt.Sprintf(`{"categoryId":%q,"name":%q}`, categoryID, name))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create subject %q: status %d, body %s", name, rec.Code, rec.Body.String())
	}
	return decodeData(t, rec)
}

func TestCreateSubjectAPI(t *testing.T) { // S04-1
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	rec := e.do(t, http.MethodPost, subjectsPath, fmt.Sprintf(`{"categoryId":%q,"name":" Networking "}`, strings.ToUpper(ti["id"].(string))))

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	data := decodeData(t, rec)
	cat := data["category"].(map[string]any)
	if data["name"] != "Networking" || data["description"] != nil || data["noteCount"] != float64(0) ||
		data["categoryId"] != ti["id"] || cat["path"] != "TI" || cat["id"] != ti["id"] || cat["color"] != "#22D3EE" {
		t.Errorf("data = %v", data)
	}
	if loc := rec.Header().Get("Location"); loc != subjectsPath+"/"+data["id"].(string) {
		t.Errorf("Location = %q", loc)
	}
}

func TestCreateSubjectInNonLeafAndNameScopeAPI(t *testing.T) { // S04-2, S04-3
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	cloud := e.createCategory(t, "Cloud", ti["id"].(string))
	e.createSubject(t, ti["id"].(string), "Networking") // TI has children
	e.createSubject(t, cloud["id"].(string), "Networking")
	body := fmt.Sprintf(`{"categoryId":%q,"name":"NETWORKING"}`, ti["id"])
	wantError(t, e.do(t, http.MethodPost, subjectsPath, body), 409, "conflict", "name", "duplicate_name")
}

func TestCreateSubjectErrorsAPI(t *testing.T) {
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	cid := ti["id"].(string)
	tests := []struct {
		name, body string
		status     int
		code       string
		field      string
		rule       string
	}{
		{"missing categoryId", `{"name":"N"}`, 422, "validation_failed", "categoryId", "required"},
		{"malformed categoryId", `{"categoryId":"x","name":"N"}`, 422, "validation_failed", "categoryId", "format"},
		{"unknown category", `{"categoryId":"` + unknownID + `","name":"N"}`, 422, "validation_failed", "categoryId", "not_found"},
		{"blank name", fmt.Sprintf(`{"categoryId":%q,"name":" "}`, cid), 422, "validation_failed", "name", "required"},
		{"long name", fmt.Sprintf(`{"categoryId":%q,"name":%q}`, cid, strings.Repeat("a", 121)), 422, "validation_failed", "name", "max_length"},
		{"control name", fmt.Sprintf(`{"categoryId":%q,"name":"a\u0000b"}`, cid), 422, "validation_failed", "name", "format"},
		{"NUL description", fmt.Sprintf(`{"categoryId":%q,"name":"N","description":"a\u0000b"}`, cid), 422, "validation_failed", "description", "format"},
		{"invalid json", `{"name":`, 422, "validation_failed", "body", "json"},
		{"too large", fmt.Sprintf(`{"categoryId":%q,"name":%q}`, cid, strings.Repeat("a", testMaxBodyBytes)), 413, "payload_too_large", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantError(t, e.do(t, http.MethodPost, subjectsPath, tc.body), tc.status, tc.code, tc.field, tc.rule)
		})
	}
}

func TestListSubjectsAPI(t *testing.T) { // S04-8
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	cloud := e.createCategory(t, "Cloud", ti["id"].(string))
	e.createSubject(t, ti["id"].(string), "zeta")
	e.createSubject(t, ti["id"].(string), "Alpha")
	e.createSubject(t, cloud["id"].(string), "Deep")

	names := func(path string) string {
		rec := e.do(t, http.MethodGet, path, "")
		if rec.Code != 200 {
			t.Fatalf("%s: status %d, body %s", path, rec.Code, rec.Body.String())
		}
		var env struct {
			Data []map[string]any `json:"data"`
			Meta map[string]any   `json:"meta"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, d := range env.Data {
			out = append(out, d["name"].(string))
		}
		if env.Meta["totalItems"] != float64(len(out)) {
			t.Errorf("meta = %v for %v", env.Meta, out)
		}
		return strings.Join(out, ",")
	}
	if got := names(subjectsPath); got != "Alpha,Deep,zeta" {
		t.Errorf("all = %s", got)
	}
	if got := names(subjectsPath + "?categoryId=" + ti["id"].(string)); got != "Alpha,zeta" {
		t.Errorf("TI only = %s", got)
	}
	if got := names(subjectsPath + "?categoryId=" + unknownID); got != "" {
		t.Errorf("unknown = %q, want empty", got)
	}
	wantError(t, e.do(t, http.MethodGet, subjectsPath+"?categoryId=nope", ""), 422, "validation_failed", "categoryId", "format")
}

func TestGetSubjectAPI(t *testing.T) {
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	s := e.createSubject(t, ti["id"].(string), "Net")
	id := s["id"].(string)

	if rec := e.do(t, http.MethodGet, subjectsPath+"/"+strings.ToUpper(id), ""); rec.Code != 200 || decodeData(t, rec)["id"] != id {
		t.Errorf("upper-case id: status %d, body %s", rec.Code, rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodGet, subjectsPath+"/"+unknownID, ""), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodGet, subjectsPath+"/not-a-uuid", ""), 404, "not_found", "", "")
}

func TestUpdateSubjectAPI(t *testing.T) { // S04-5, S04-6, S04-7
	e := newAPIEnv()
	a, b := e.createCategory(t, "A", ""), e.createCategory(t, "B", "")
	s := e.createSubject(t, a["id"].(string), "Net")
	e.createSubject(t, b["id"].(string), "Taken")
	path := subjectsPath + "/" + s["id"].(string)

	rec := e.do(t, http.MethodPatch, path, fmt.Sprintf(`{"name":"Networking","description":"d","categoryId":%q}`, b["id"]))
	data := decodeData(t, rec)
	if rec.Code != 200 || data["name"] != "Networking" || data["description"] != "d" || data["category"].(map[string]any)["id"] != b["id"] {
		t.Fatalf("update: status %d, body %s", rec.Code, rec.Body.String())
	}
	if rec := e.do(t, http.MethodPatch, path, `{"description":null}`); rec.Code != 200 || decodeData(t, rec)["description"] != nil {
		t.Errorf("clear description: %d %s", rec.Code, rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodPatch, path, `{"name":"taken"}`), 409, "conflict", "name", "duplicate_name")
	wantError(t, e.do(t, http.MethodPatch, path, `{}`), 422, "validation_failed", "body", "required")
	wantError(t, e.do(t, http.MethodPatch, path, `{"name":null}`), 422, "validation_failed", "name", "required")
	wantError(t, e.do(t, http.MethodPatch, path, `{"categoryId":null}`), 422, "validation_failed", "categoryId", "required")
	wantError(t, e.do(t, http.MethodPatch, path, `{"categoryId":"zz"}`), 422, "validation_failed", "categoryId", "format")
	wantError(t, e.do(t, http.MethodPatch, path, `{"categoryId":"`+unknownID+`"}`), 422, "validation_failed", "categoryId", "not_found")
	wantError(t, e.do(t, http.MethodPatch, path, `{"description":"`+strings.Repeat("a", testMaxBodyBytes)+`"}`), 413, "payload_too_large", "", "")
	wantError(t, e.do(t, http.MethodPatch, subjectsPath+"/"+unknownID, `{"name":"X"}`), 404, "not_found", "", "")
}

func TestDeleteSubjectAndCategoryBlockAPI(t *testing.T) { // S04-9, S04-10, S04-11
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	cpath := "/api/v1/categories/" + ti["id"].(string)
	s := e.createSubject(t, ti["id"].(string), "Net")
	spath := subjectsPath + "/" + s["id"].(string)

	if rec := e.do(t, http.MethodGet, cpath, ""); decodeData(t, rec)["subjectCount"] != float64(1) {
		t.Errorf("subjectCount != 1: %s", rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodDelete, cpath, ""), 409, "conflict", "id", "in_use")

	if rec := e.do(t, http.MethodDelete, spath, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body %s", rec.Code, rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodGet, spath, ""), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodPatch, spath, `{"name":"X"}`), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodDelete, spath, ""), 404, "not_found", "", "")
	if rec := e.do(t, http.MethodGet, cpath, ""); decodeData(t, rec)["subjectCount"] != float64(0) {
		t.Errorf("subjectCount != 0 after delete: %s", rec.Body.String())
	}
	e.createSubject(t, ti["id"].(string), "Net") // name freed
	// the new subject blocks the category again; remove it to finish
	if rec := e.do(t, http.MethodDelete, cpath, ""); rec.Code != 409 {
		t.Errorf("status = %d, want 409", rec.Code)
	}
}

func TestDeleteSubjectWithAliveExamsIsInUseAPI(t *testing.T) { // S02-7
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	s := e.createSubject(t, ti["id"].(string), "AWS")
	spath := subjectsPath + "/" + s["id"].(string)
	e.exams.Counts[s["id"].(string)] = 1 // an alive exam, owned by the exams context

	wantError(t, e.do(t, http.MethodDelete, spath, ""), 409, "conflict", "id", "in_use")
	if rec := e.do(t, http.MethodGet, spath, ""); rec.Code != http.StatusOK {
		t.Fatalf("the blocked subject must remain readable, status = %d", rec.Code)
	}

	e.exams.Counts[s["id"].(string)] = 0 // the exam was deleted

	if rec := e.do(t, http.MethodDelete, spath, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body %s", rec.Code, rec.Body.String())
	}
}

// decodeList decodes a { data: [...] } list response.
func decodeList(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body.String(), err)
	}
	return env.Data
}

func jsonString(s string) (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}
