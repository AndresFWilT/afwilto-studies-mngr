package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/afwilto/studies-mngr/internal/studies/app/usecase"
	"github.com/afwilto/studies-mngr/internal/studies/testsupport"
)

const testMaxBodyBytes = 512

type apiEnv struct {
	router http.Handler
	exams  *testsupport.FakeSubjectExamCounter // alive exams per subject, set by tests
}

func newAPIEnv() apiEnv {
	categories := testsupport.NewFakeCategoryRepo()
	kinds := testsupport.NewFakeNoteKindRepo()
	subjects := testsupport.NewFakeSubjectRepo(categories)
	notes := testsupport.NewFakeNoteRepo(subjects, kinds)
	tags := testsupport.FakeTagQuery{Notes: notes}
	clock := &testsupport.StepClock{}
	ids := &testsupport.SeqIDGen{}
	exams := testsupport.NewFakeSubjectExamCounter()
	return apiEnv{exams: exams, router: NewRouter(RouterDeps{
		Pinger:         stubPinger{},
		AllowOrigins:   []string{"http://localhost:3000"},
		MaxBodyBytes:   testMaxBodyBytes,
		CreateCategory: usecase.NewCreateCategoryService(categories, clock, ids),
		GetCategory:    usecase.NewGetCategoryService(categories),
		ListCategories: usecase.NewListCategoriesService(categories),
		UpdateCategory: usecase.NewUpdateCategoryService(categories, clock),
		DeleteCategory: usecase.NewDeleteCategoryService(categories, subjects, clock),

		ListNotesByTag:   usecase.NewListNotesByTagService(notes),
		ListTags:         usecase.NewListTagsService(tags),
		ListSubjectNotes: usecase.NewListSubjectNotesService(notes, subjects),
		CreateNote:       usecase.NewCreateNoteService(notes, subjects, kinds, clock, ids),
		GetNote:          usecase.NewGetNoteService(notes),
		UpdateNote:       usecase.NewUpdateNoteService(notes, subjects, kinds, clock),
		DeleteNote:       usecase.NewDeleteNoteService(notes, clock),

		ListSubjects:  usecase.NewListSubjectsService(subjects),
		CreateSubject: usecase.NewCreateSubjectService(subjects, categories, clock, ids),
		GetSubject:    usecase.NewGetSubjectService(subjects),
		UpdateSubject: usecase.NewUpdateSubjectService(subjects, categories, clock),
		DeleteSubject: usecase.NewDeleteSubjectService(subjects, notes, exams, clock),

		ListNoteKinds:  usecase.NewListNoteKindsService(kinds),
		CreateNoteKind: usecase.NewCreateNoteKindService(kinds, clock, ids),
		GetNoteKind:    usecase.NewGetNoteKindService(kinds),
		RenameNoteKind: usecase.NewRenameNoteKindService(kinds, clock),
		DeleteNoteKind: usecase.NewDeleteNoteKindService(kinds, notes, clock),
	})}
}

// do sends a request and asserts the response conforms to the contract.
func (e apiEnv) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != "" {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := serve(e.router, req)
	assertConformsToContract(t, method, path, rec)
	return rec
}

func decodeData(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body.String(), err)
	}
	return envelope.Data
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Details []struct {
			Field string `json:"field"`
			Rule  string `json:"rule"`
		} `json:"details"`
	} `json:"error"`
}

func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code, field, rule string) {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, status, rec.Body.String())
	}
	var env errorEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body.String(), err)
	}
	if env.Error.Code != code {
		t.Errorf("error.code = %q, want %q", env.Error.Code, code)
	}
	if field == "" {
		return
	}
	for _, d := range env.Error.Details {
		if d.Field == field && d.Rule == rule {
			return
		}
	}
	t.Errorf("details = %+v, want field %q rule %q", env.Error.Details, field, rule)
}

func (e apiEnv) createCategory(t *testing.T, name string, parentID string) map[string]any {
	t.Helper()
	body := fmt.Sprintf(`{"name":%q,"color":"#22d3ee"}`, name)
	if parentID != "" {
		body = fmt.Sprintf(`{"name":%q,"color":"#22d3ee","parentId":%q}`, name, parentID)
	}
	rec := e.do(t, http.MethodPost, "/api/v1/categories", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %q: status %d, body %s", name, rec.Code, rec.Body.String())
	}
	return decodeData(t, rec)
}

const unknownID = "00000000-0000-7000-8000-999999999999"

// ── Create ──────────────────────────────────────────────────────────────────

func TestCreateRootCategoryAPI(t *testing.T) {
	e := newAPIEnv()
	rec := e.do(t, http.MethodPost, "/api/v1/categories", `{"name":"TI","color":"#ff9900"}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	data := decodeData(t, rec)
	if data["color"] != "#FF9900" || data["parentId"] != nil || data["path"] != "TI" || data["depth"] != float64(1) || data["subjectCount"] != float64(0) {
		t.Errorf("data = %v", data)
	}
	if loc := rec.Header().Get("Location"); loc != "/api/v1/categories/"+data["id"].(string) {
		t.Errorf("Location = %q", loc)
	}
}

func TestCreateNestedCategoryAPI(t *testing.T) {
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	cloud := e.createCategory(t, "Cloud", ti["id"].(string))
	aws := e.createCategory(t, "AWS", cloud["id"].(string))
	if aws["path"] != "TI / Cloud / AWS" || aws["depth"] != float64(3) || aws["parentId"] != cloud["id"] {
		t.Errorf("aws = %v", aws)
	}
}

func TestCreateCategoryErrorsAPI(t *testing.T) {
	e := newAPIEnv()
	e.createCategory(t, "TI", "")

	cases := []struct {
		name, body   string
		status       int
		code, f, rul string
	}{
		{"duplicate root", `{"name":"ti","color":"#000000"}`, 409, "conflict", "name", "duplicate_name"},
		{"bad color", `{"name":"X","color":"blue"}`, 422, "validation_failed", "color", "format"},
		{"empty name", `{"name":"  ","color":"#000000"}`, 422, "validation_failed", "name", "required"},
		{"malformed parentId", `{"name":"X","color":"#000000","parentId":"abc"}`, 422, "validation_failed", "parentId", "format"},
		{"unknown parentId", `{"name":"X","color":"#000000","parentId":"` + unknownID + `"}`, 422, "validation_failed", "parentId", "not_found"},
		{"invalid json", `{"name":`, 422, "validation_failed", "body", "json"},
		{"too large", `{"name":"` + strings.Repeat("a", testMaxBodyBytes) + `","color":"#000000"}`, 413, "payload_too_large", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantError(t, e.do(t, http.MethodPost, "/api/v1/categories", tc.body), tc.status, tc.code, tc.f, tc.rul)
		})
	}
}

// ── Get / List ──────────────────────────────────────────────────────────────

func TestGetAndListCategoriesAPI(t *testing.T) {
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	e.createCategory(t, "Cloud", ti["id"].(string))
	e.createCategory(t, "art", "")

	rec := e.do(t, http.MethodGet, "/api/v1/categories/"+ti["id"].(string), "")
	if rec.Code != http.StatusOK || decodeData(t, rec)["name"] != "TI" {
		t.Errorf("get: %d %s", rec.Code, rec.Body.String())
	}

	rec = e.do(t, http.MethodGet, "/api/v1/categories", "")
	var list struct {
		Data []map[string]any `json:"data"`
		Meta struct {
			TotalItems int `json:"totalItems"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if list.Meta.TotalItems != 3 || len(list.Data) != 3 {
		t.Fatalf("list = %s", rec.Body.String())
	}
	for i, want := range []string{"art", "TI", "TI / Cloud"} {
		if list.Data[i]["path"] != want {
			t.Errorf("list[%d].path = %v, want %q", i, list.Data[i]["path"], want)
		}
	}
}

func TestGetCategoryNotFoundAPI(t *testing.T) {
	e := newAPIEnv()
	wantError(t, e.do(t, http.MethodGet, "/api/v1/categories/"+unknownID, ""), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodGet, "/api/v1/categories/not-a-uuid", ""), 404, "not_found", "", "")
}

func TestEmptyCategoryListIsAnArray(t *testing.T) {
	e := newAPIEnv()
	rec := e.do(t, http.MethodGet, "/api/v1/categories", "")
	if got := strings.TrimSpace(rec.Body.String()); got != `{"data":[],"meta":{"totalItems":0}}` {
		t.Errorf("body = %s", got)
	}
}

// ── Update ──────────────────────────────────────────────────────────────────

func TestUpdateCategoryAPI(t *testing.T) {
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	cloud := e.createCategory(t, "Cloud", ti["id"].(string))
	cloudPath := "/api/v1/categories/" + cloud["id"].(string)

	rec := e.do(t, http.MethodPatch, cloudPath, `{"color":"#34d399"}`)
	if data := decodeData(t, rec); rec.Code != 200 || data["color"] != "#34D399" || data["parentId"] != ti["id"] {
		t.Errorf("recolor (parentId omitted keeps parent): %d %s", rec.Code, rec.Body.String())
	}

	rec = e.do(t, http.MethodPatch, cloudPath, `{"parentId":null}`)
	if data := decodeData(t, rec); rec.Code != 200 || data["parentId"] != nil || data["path"] != "Cloud" {
		t.Errorf("move to root: %d %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateCategoryErrorsAPI(t *testing.T) {
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	cloud := e.createCategory(t, "Cloud", ti["id"].(string))
	tiPath := "/api/v1/categories/" + ti["id"].(string)

	wantError(t, e.do(t, http.MethodPatch, tiPath, `{}`), 422, "validation_failed", "body", "required")
	wantError(t, e.do(t, http.MethodPatch, tiPath, `{"parentId":"`+cloud["id"].(string)+`"}`), 422, "validation_failed", "parentId", "cycle")
	wantError(t, e.do(t, http.MethodPatch, tiPath, `{"name":null}`), 422, "validation_failed", "name", "required")
	wantError(t, e.do(t, http.MethodPatch, "/api/v1/categories/"+unknownID, `{"name":"X"}`), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodPatch, "/api/v1/categories/nope", `{"name":"X"}`), 404, "not_found", "", "")
}

// ── Delete ──────────────────────────────────────────────────────────────────

func TestDeleteCategoryAPI(t *testing.T) {
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	cloud := e.createCategory(t, "Cloud", ti["id"].(string))
	tiPath := "/api/v1/categories/" + ti["id"].(string)
	cloudPath := "/api/v1/categories/" + cloud["id"].(string)

	wantError(t, e.do(t, http.MethodDelete, tiPath, ""), 409, "conflict", "id", "in_use")

	if rec := e.do(t, http.MethodDelete, cloudPath, ""); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("delete leaf: %d %q", rec.Code, rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodGet, cloudPath, ""), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodDelete, cloudPath, ""), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodPatch, cloudPath, `{"name":"X"}`), 404, "not_found", "", "")

	if rec := e.do(t, http.MethodDelete, tiPath, ""); rec.Code != http.StatusNoContent {
		t.Errorf("delete emptied parent: %d %s", rec.Code, rec.Body.String())
	}
}

// ── Review fixes: ids, control characters, body limits (S02-3, S02-6, S02-7) ──

func TestCategoryIDsAreCaseInsensitive(t *testing.T) {
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	cloud := e.createCategory(t, "Cloud", ti["id"].(string))
	tiUpper := strings.ToUpper(ti["id"].(string))
	cloudUpper := strings.ToUpper(cloud["id"].(string))
	if tiUpper == ti["id"] {
		t.Fatal("test ids must contain hex letters to exercise case handling")
	}

	rec := e.do(t, http.MethodGet, "/api/v1/categories/"+tiUpper, "")
	if rec.Code != http.StatusOK || decodeData(t, rec)["id"] != ti["id"] {
		t.Errorf("uppercase GET: %d %s, want 200 with the lowercase id", rec.Code, rec.Body.String())
	}
	wantError(t, e.do(t, http.MethodPatch, "/api/v1/categories/"+tiUpper, `{"parentId":"`+cloud["id"].(string)+`"}`),
		422, "validation_failed", "parentId", "cycle")
	wantError(t, e.do(t, http.MethodPatch, "/api/v1/categories/"+tiUpper, `{"parentId":"`+tiUpper+`"}`),
		422, "validation_failed", "parentId", "cycle")

	other := e.createCategory(t, "Other", "")
	rec = e.do(t, http.MethodPatch, "/api/v1/categories/"+cloudUpper, `{"parentId":"`+strings.ToUpper(other["id"].(string))+`"}`)
	if data := decodeData(t, rec); rec.Code != 200 || data["parentId"] != other["id"] || data["id"] != cloud["id"] {
		t.Errorf("uppercase move: %d %s, want lowercase ids", rec.Code, rec.Body.String())
	}
}

func TestCategoryNameRejectsControlCharactersAPI(t *testing.T) {
	e := newAPIEnv()
	wantError(t, e.do(t, http.MethodPost, "/api/v1/categories", `{"name":"a\u0000b","color":"#112233"}`),
		422, "validation_failed", "name", "format")
}

func TestPatchMalformedParentIDAPI(t *testing.T) {
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	wantError(t, e.do(t, http.MethodPatch, "/api/v1/categories/"+ti["id"].(string), `{"parentId":"nope"}`),
		422, "validation_failed", "parentId", "format")
}

func TestBodyLimitsAPI(t *testing.T) {
	e := newAPIEnv()
	ti := e.createCategory(t, "TI", "")
	tiPath := "/api/v1/categories/" + ti["id"].(string)
	big := strings.Repeat(" ", testMaxBodyBytes)

	cases := []struct {
		name, method, path, body string
		streamed                 bool
		status                   int
		code                     string
	}{
		{"post junk over limit", http.MethodPost, "/api/v1/categories", strings.Repeat("x", testMaxBodyBytes+1), false, 413, "payload_too_large"},
		{"post valid json then padding over limit", http.MethodPost, "/api/v1/categories", `{"name":"X","color":"#000000"}` + big, false, 413, "payload_too_large"},
		{"post streamed junk over limit", http.MethodPost, "/api/v1/categories", strings.Repeat("x", testMaxBodyBytes+1), true, 413, "payload_too_large"},
		{"post streamed json then padding", http.MethodPost, "/api/v1/categories", `{"name":"X","color":"#000000"}` + big, true, 413, "payload_too_large"},
		{"post trailing garbage", http.MethodPost, "/api/v1/categories", `{"name":"X","color":"#000000"} garbage`, false, 422, "validation_failed"},
		{"post two values", http.MethodPost, "/api/v1/categories", `{"name":"X","color":"#000000"}{}`, false, 422, "validation_failed"},
		{"patch invalid json", http.MethodPatch, tiPath, `{"name":`, false, 422, "validation_failed"},
		{"patch over limit", http.MethodPatch, tiPath, `{"name":"` + strings.Repeat("a", testMaxBodyBytes) + `"}`, false, 413, "payload_too_large"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.streamed {
				req.ContentLength = -1 // chunked: size unknown up front
			}
			rec := serve(e.router, req)
			assertConformsToContract(t, tc.method, tc.path, rec)
			wantError(t, rec, tc.status, tc.code, "", "")
		})
	}

	// nothing was created by any of the rejected requests
	rec := e.do(t, http.MethodGet, "/api/v1/categories", "")
	if !strings.Contains(rec.Body.String(), `"totalItems":1`) {
		t.Errorf("list after rejected requests = %s, want only TI", rec.Body.String())
	}
}
