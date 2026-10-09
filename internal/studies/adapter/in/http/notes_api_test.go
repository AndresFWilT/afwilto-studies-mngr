package http

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

const notesPath = "/api/v1/notes"

// noteFixture is an API environment with one subject and one kind.
type noteFixture struct {
	apiEnv
	subjectID, kindID string
	listPath          string
}

func newNoteFixture(t *testing.T) noteFixture {
	t.Helper()
	e := newAPIEnv()
	cat := e.createCategory(t, "TI", "")
	subj := e.createSubject(t, cat["id"].(string), "AWS")
	kind := e.createKind(t, "Study notes")
	sid := subj["id"].(string)
	return noteFixture{apiEnv: e, subjectID: sid, kindID: kind["id"].(string), listPath: subjectsPath + "/" + sid + "/notes"}
}

func (f noteFixture) body(title, extra string) string {
	b := fmt.Sprintf(`{"title":%q,"kindId":%q`, title, f.kindID)
	if extra != "" {
		b += "," + extra
	}
	return b + "}"
}

func (f noteFixture) createNote(t *testing.T, title string) map[string]any {
	t.Helper()
	rec := f.do(t, http.MethodPost, f.listPath, f.body(title, ""))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create note %q: status %d, body %s", title, rec.Code, rec.Body.String())
	}
	return decodeData(t, rec)
}

func TestCreateAndGetNoteAPI(t *testing.T) { // S05-1, AC-B15
	f := newNoteFixture(t)
	content := "# Storage\n\n$$E=mc^2$$\n\n```go\nfmt.Println(\"hi\")\n```\n\n```mermaid\ngraph TD; A-->B\n```\n"
	raw, _ := jsonString(content)
	rec := f.do(t, http.MethodPost, f.listPath, f.body("  Storage ", `"content":`+raw))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	data := decodeData(t, rec)
	kind := data["kind"].(map[string]any)
	if data["title"] != "Storage" || data["version"] != float64(1) || data["content"] != content ||
		data["subjectId"] != f.subjectID || kind["id"] != f.kindID || kind["name"] != "Study notes" {
		t.Errorf("data = %v", data)
	}
	if tags, ok := data["tags"].([]any); !ok || len(tags) != 0 {
		t.Errorf("tags = %v", data["tags"])
	}
	if links, ok := data["slabLinks"].([]any); !ok || len(links) != 0 {
		t.Errorf("slabLinks = %v", data["slabLinks"])
	}
	if loc := rec.Header().Get("Location"); loc != notesPath+"/"+data["id"].(string) {
		t.Errorf("Location = %q", loc)
	}
	got := decodeData(t, f.do(t, http.MethodGet, notesPath+"/"+strings.ToUpper(data["id"].(string)), ""))
	if got["content"] != content {
		t.Errorf("content changed on read: %q", got["content"])
	}
}

func TestCreateNoteErrorsAPI(t *testing.T) {
	f := newNoteFixture(t)
	f.createNote(t, "Storage")
	big := strings.Repeat("a", 200001)
	tests := []struct {
		name, path, body string
		status           int
		code             string
		field            string
		rule             string
	}{
		{"duplicate title", f.listPath, f.body("STORAGE", ""), 409, "conflict", "title", "duplicate_name"},
		{"blank title", f.listPath, f.body(" ", ""), 422, "validation_failed", "title", "required"},
		{"missing kindId", f.listPath, `{"title":"T"}`, 422, "validation_failed", "kindId", "required"},
		{"malformed kindId", f.listPath, `{"title":"T","kindId":"x"}`, 422, "validation_failed", "kindId", "format"},
		{"unknown kind", f.listPath, `{"title":"T","kindId":"` + unknownID + `"}`, 422, "validation_failed", "kindId", "not_found"},
		{"unknown subject", subjectsPath + "/" + unknownID + "/notes", f.body("T", ""), 404, "not_found", "", ""},
		{"malformed subject id", subjectsPath + "/nope/notes", f.body("T", ""), 404, "not_found", "", ""},
		{"tags not implemented", f.listPath, f.body("T", `"tags":["a"]`), 422, "validation_failed", "tags", "not_implemented"},
		{"slabLinks not implemented", f.listPath, f.body("T", `"slabLinks":[{"label":"l","path":"/slab/a"}]`), 422, "validation_failed", "slabLinks", "not_implemented"},
		{"invalid json", f.listPath, `{"title":`, 422, "validation_failed", "body", "json"},
		{"body over limit", f.listPath, f.body("T", `"content":"`+big+`"`), 413, "payload_too_large", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantError(t, f.do(t, http.MethodPost, tc.path, tc.body), tc.status, tc.code, tc.field, tc.rule)
		})
	}
	// Empty tags / slabLinks are accepted.
	if rec := f.do(t, http.MethodPost, f.listPath, f.body("Empty arrays", `"tags":[],"slabLinks":[]`)); rec.Code != 201 {
		t.Errorf("empty arrays: status %d, body %s", rec.Code, rec.Body.String())
	}
}

func TestListNotesSortingAPI(t *testing.T) { // S05-2, AC-B16
	f := newNoteFixture(t)
	compute, storage, networking := f.createNote(t, "Compute"), f.createNote(t, "Storage"), f.createNote(t, "Networking")
	for _, n := range []map[string]any{networking, storage, compute} { // updated order: Networking < Storage < Compute
		rec := f.do(t, http.MethodPatch, notesPath+"/"+n["id"].(string), `{"version":1,"content":"x"}`)
		if rec.Code != 200 {
			t.Fatalf("touch: %d %s", rec.Code, rec.Body.String())
		}
	}
	order := func(query string) (string, int) {
		rec := f.do(t, http.MethodGet, f.listPath+query, "")
		if rec.Code != 200 {
			return rec.Body.String(), rec.Code
		}
		var names []string
		for _, item := range decodeList(t, rec) {
			if _, has := item["content"]; has {
				t.Errorf("list item has content: %v", item)
			}
			names = append(names, item["title"].(string))
		}
		return strings.Join(names, ","), 200
	}
	tests := []struct{ query, want string }{
		{"", "Compute,Networking,Storage"},
		{"?sort=title&order=desc", "Storage,Networking,Compute"},
		{"?sort=updatedAt&order=desc", "Compute,Storage,Networking"},
		{"?sort=createdAt&order=desc", "Networking,Storage,Compute"},
		{"?sort=createdAt&order=asc", "Compute,Storage,Networking"},
	}
	for _, tc := range tests {
		if got, status := order(tc.query); status != 200 || got != tc.want {
			t.Errorf("%q: got %s (%d), want %s", tc.query, got, status, tc.want)
		}
	}
	wantError(t, f.do(t, http.MethodGet, f.listPath+"?sort=size", ""), 422, "validation_failed", "sort", "enum")
	wantError(t, f.do(t, http.MethodGet, f.listPath+"?order=bogus", ""), 422, "validation_failed", "order", "enum")
	wantError(t, f.do(t, http.MethodGet, f.listPath+"?tag=iam", ""), 422, "validation_failed", "tag", "not_implemented")
	wantError(t, f.do(t, http.MethodGet, subjectsPath+"/"+unknownID+"/notes", ""), 404, "not_found", "", "")
}

func TestUpdateNoteVersionGuardAPI(t *testing.T) { // S05-3, S05-10, AC-B17
	f := newNoteFixture(t)
	n := f.createNote(t, "Storage")
	path := notesPath + "/" + n["id"].(string)

	rec := f.do(t, http.MethodPatch, path, `{"version":1,"content":"v2"}`)
	if data := decodeData(t, rec); rec.Code != 200 || data["version"] != float64(2) || data["content"] != "v2" {
		t.Fatalf("first edit: %d %s", rec.Code, rec.Body.String())
	}
	wantError(t, f.do(t, http.MethodPatch, path, `{"version":1,"content":"lost"}`), 409, "conflict", "version", "stale_version")
	if got := decodeData(t, f.do(t, http.MethodGet, path, "")); got["content"] != "v2" || got["version"] != float64(2) {
		t.Errorf("stale edit changed the note: %v", got)
	}
	wantError(t, f.do(t, http.MethodPatch, path, `{"content":"x"}`), 422, "validation_failed", "version", "required")
	wantError(t, f.do(t, http.MethodPatch, path, `{"version":0,"content":"x"}`), 422, "validation_failed", "version", "min")
	wantError(t, f.do(t, http.MethodPatch, path, `{"version":2}`), 422, "validation_failed", "body", "required")
	wantError(t, f.do(t, http.MethodPatch, path, `{"version":2,"title":null}`), 422, "validation_failed", "title", "required")
	wantError(t, f.do(t, http.MethodPatch, path, `{"version":2,"kindId":"`+unknownID+`"}`), 422, "validation_failed", "kindId", "not_found")
	wantError(t, f.do(t, http.MethodPatch, path, `{"version":2,"subjectId":"`+unknownID+`"}`), 422, "validation_failed", "subjectId", "not_found")
	wantError(t, f.do(t, http.MethodPatch, path, `{"version":2,"subjectId":"zz"}`), 422, "validation_failed", "subjectId", "format")
	wantError(t, f.do(t, http.MethodPatch, path, `{"version":2,"tags":["a"]}`), 422, "validation_failed", "tags", "not_implemented")
	wantError(t, f.do(t, http.MethodPatch, path, `{"version":"2","content":"x"}`), 422, "validation_failed", "body", "json")
	wantError(t, f.do(t, http.MethodPatch, notesPath+"/"+unknownID, `{"version":1,"content":"x"}`), 404, "not_found", "", "")
}

func TestContentBoundaryAPI(t *testing.T) { // S05-4, AC-B18 (the body limit is 512 in tests, so use a bigger env)
	f := newNoteFixture(t)
	n := f.createNote(t, "Storage")
	path := notesPath + "/" + n["id"].(string)
	// Under the 512-byte test limit only the 413 path is reachable over HTTP;
	// the 200,000 / 200,001 character boundary is covered by domain and use-case tests.
	wantError(t, f.do(t, http.MethodPatch, path, `{"version":1,"content":"`+strings.Repeat("a", testMaxBodyBytes)+`"}`), 413, "payload_too_large", "", "")
}

func TestMoveNoteAndCountsAPI(t *testing.T) { // S05-6, S05-7
	f := newNoteFixture(t)
	n := f.createNote(t, "Storage")
	cat := f.createCategory(t, "Other", "")
	other := f.createSubject(t, cat["id"].(string), "Azure")
	if rec := f.do(t, http.MethodPost, subjectsPath+"/"+other["id"].(string)+"/notes", f.body("storage", "")); rec.Code != 201 {
		t.Fatalf("same title in another subject: %d %s", rec.Code, rec.Body.String())
	}
	path := notesPath + "/" + n["id"].(string)
	wantError(t, f.do(t, http.MethodPatch, path, fmt.Sprintf(`{"version":1,"subjectId":%q}`, other["id"])), 409, "conflict", "title", "duplicate_name")

	subj := decodeData(t, f.do(t, http.MethodGet, subjectsPath+"/"+f.subjectID, ""))
	if subj["noteCount"] != float64(1) {
		t.Errorf("subject noteCount = %v, want 1", subj["noteCount"])
	}
	kinds := decodeList(t, f.do(t, http.MethodGet, kindsPath, ""))
	if kinds[0]["noteCount"] != float64(2) {
		t.Errorf("kind noteCount = %v, want 2", kinds[0]["noteCount"])
	}
}

func TestDeleteNoteAndBlockedParentsAPI(t *testing.T) { // S05-5, S05-8, S05-9
	f := newNoteFixture(t)
	n := f.createNote(t, "Storage")
	path := notesPath + "/" + n["id"].(string)
	spath := subjectsPath + "/" + f.subjectID
	kpath := kindsPath + "/" + f.kindID

	wantError(t, f.do(t, http.MethodDelete, spath, ""), 409, "conflict", "id", "in_use")
	wantError(t, f.do(t, http.MethodDelete, kpath, ""), 409, "conflict", "id", "in_use")

	if rec := f.do(t, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	wantError(t, f.do(t, http.MethodGet, path, ""), 404, "not_found", "", "")
	wantError(t, f.do(t, http.MethodPatch, path, `{"version":1,"content":"x"}`), 404, "not_found", "", "")
	wantError(t, f.do(t, http.MethodDelete, path, ""), 404, "not_found", "", "")
	if got := decodeList(t, f.do(t, http.MethodGet, f.listPath, "")); len(got) != 0 {
		t.Errorf("deleted note still listed: %v", got)
	}
	f.createNote(t, "Storage") // title freed

	f.do(t, http.MethodDelete, notesPath+"/"+f.createNote(t, "Other")["id"].(string), "") // keep counts simple
	// kind can be deleted once no alive note uses it
	rec := f.do(t, http.MethodGet, f.listPath, "")
	for _, item := range decodeList(t, rec) {
		f.do(t, http.MethodDelete, notesPath+"/"+item["id"].(string), "")
	}
	if rec := f.do(t, http.MethodDelete, kpath, ""); rec.Code != http.StatusNoContent {
		t.Errorf("kind delete after notes removed: %d %s", rec.Code, rec.Body.String())
	}
	wantError(t, f.do(t, http.MethodPost, f.listPath, f.body("T", "")), 422, "validation_failed", "kindId", "not_found")
}
