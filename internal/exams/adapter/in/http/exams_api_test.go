package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/afwilto/studies-mngr/internal/exams/app/usecase"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

const (
	testMaxBodyBytes = 512

	subjectA  = "0abcdef0-0000-7000-8000-00000000a001"
	subjectB  = "0abcdef0-0000-7000-8000-00000000b002"
	deadSub   = "0abcdef0-0000-7000-8000-00000000d003" // a subject that is not alive
	unknownID = "00000000-0000-7000-8000-999999999999"
)

type apiEnv struct {
	router http.Handler
}

func newAPIEnv() apiEnv {
	subjects := testsupport.NewFakeSubjectDirectory(subjectA, subjectB)
	repo := testsupport.NewFakeExamRepo(subjects)
	clock := &testsupport.StepClock{}
	ids := &testsupport.SeqIDGen{}
	return apiEnv{router: httpx.NewServer(httpx.Config{MaxBodyBytes: testMaxBodyBytes}, Register(Deps{
		CreateExam:       usecase.NewCreateExamService(repo, subjects, clock, ids),
		ListSubjectExams: usecase.NewListSubjectExamsService(repo, subjects),
		GetExam:          usecase.NewGetExamService(repo),
		UpdateExam:       usecase.NewUpdateExamService(repo, clock),
		DeleteExam:       usecase.NewDeleteExamService(repo, clock),
	}))}
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
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
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

func decodeList(t *testing.T, rec *httptest.ResponseRecorder) []map[string]any {
	t.Helper()
	var envelope struct {
		Data []map[string]any `json:"data"`
		Meta map[string]any   `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body.String(), err)
	}
	if envelope.Meta["totalItems"] != float64(len(envelope.Data)) {
		t.Errorf("meta.totalItems = %v, want %d", envelope.Meta["totalItems"], len(envelope.Data))
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

func decodeError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) errorEnvelope {
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
	return env
}

// wantError asserts the status, the error code and that details carry field/rule.
func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code, field, rule string) {
	t.Helper()
	env := decodeError(t, rec, status, code)
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

func subjectExamsPath(subjectID string) string { return "/api/v1/subjects/" + subjectID + "/exams" }
func examPath(id string) string                { return "/api/v1/exams/" + id }

func examBody(name string) string {
	return fmt.Sprintf(`{"name":%q,"questionCount":65,"timeLimitMin":130,"passPercent":72}`, name)
}

func (e apiEnv) createExam(t *testing.T, subjectID, name string) map[string]any {
	t.Helper()
	rec := e.do(t, http.MethodPost, subjectExamsPath(subjectID), examBody(name))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create %q: status %d, body %s", name, rec.Code, rec.Body.String())
	}
	return decodeData(t, rec)
}

func TestCreateExamAPI(t *testing.T) { // S02-1
	e := newAPIEnv()

	rec := e.do(t, http.MethodPost, subjectExamsPath(subjectA), `{"name":"SAA-C03","questionCount":65,"timeLimitMin":130,"passPercent":72}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	data := decodeData(t, rec)
	if data["name"] != "SAA-C03" || data["subjectId"] != subjectA || data["questionCount"] != float64(65) ||
		data["timeLimitMin"] != float64(130) || data["passPercent"] != float64(72) ||
		data["version"] != float64(1) || data["questionTotal"] != float64(0) {
		t.Errorf("data = %v", data)
	}
	if classifications, ok := data["classifications"].([]any); !ok || len(classifications) != 0 {
		t.Errorf("classifications = %v, want an empty array", data["classifications"])
	}
	if _, present := data["lastSimulation"]; present {
		t.Errorf("lastSimulation must be absent until slice 06b, got %v", data["lastSimulation"])
	}
	if loc := rec.Header().Get("Location"); loc != examPath(data["id"].(string)) {
		t.Errorf("Location = %q", loc)
	}
}

func TestCreateExamTrimsTheName(t *testing.T) {
	e := newAPIEnv()

	data := e.createExam(t, subjectA, "  SAA-C03  ")

	if data["name"] != "SAA-C03" {
		t.Errorf("name = %v", data["name"])
	}
}

func TestCreateExamDuplicateNameAPI(t *testing.T) { // S02-2
	e := newAPIEnv()
	e.createExam(t, subjectA, "SAA-C03")

	for _, name := range []string{"saa-c03", "  SAA-C03 ", "Saa-C03"} {
		wantError(t, e.do(t, http.MethodPost, subjectExamsPath(subjectA), examBody(name)), 409, "conflict", "name", "duplicate_name")
	}
	e.createExam(t, subjectB, "SAA-C03") // another subject
}

func TestCreateExamRangesAPI(t *testing.T) { // S02-3
	e := newAPIEnv()

	rec := e.do(t, http.MethodPost, subjectExamsPath(subjectA), `{"name":"X","questionCount":0,"timeLimitMin":601,"passPercent":101}`)

	env := decodeError(t, rec, 422, "validation_failed")
	if len(env.Error.Details) != 3 {
		t.Fatalf("details = %+v, want one violation per field", env.Error.Details)
	}
	for _, field := range []string{"questionCount", "timeLimitMin", "passPercent"} {
		wantError(t, rec, 422, "validation_failed", field, "range")
	}
}

func TestCreateExamBoundaryValuesAreAccepted(t *testing.T) { // BR-E2
	e := newAPIEnv()

	for i, body := range []string{
		`{"name":"min","questionCount":1,"timeLimitMin":1,"passPercent":1}`,
		`{"name":"max","questionCount":200,"timeLimitMin":600,"passPercent":100}`,
	} {
		if rec := e.do(t, http.MethodPost, subjectExamsPath(subjectA), body); rec.Code != http.StatusCreated {
			t.Errorf("case %d: status = %d, body %s", i, rec.Code, rec.Body.String())
		}
	}
}

func TestCreateExamMissingFieldsAPI(t *testing.T) { // a missing field is "required", not a range error
	e := newAPIEnv()

	rec := e.do(t, http.MethodPost, subjectExamsPath(subjectA), `{}`)

	for _, field := range []string{"name", "questionCount", "timeLimitMin", "passPercent"} {
		wantError(t, rec, 422, "validation_failed", field, "required")
	}
}

func TestCreateExamBadJSONAPI(t *testing.T) {
	e := newAPIEnv()

	for name, body := range map[string]string{
		"not json":         `{"name":`,
		"fractional count": `{"name":"X","questionCount":65.5,"timeLimitMin":130,"passPercent":72}`,
		"string number":    `{"name":"X","questionCount":"65","timeLimitMin":130,"passPercent":72}`,
	} {
		rec := e.do(t, http.MethodPost, subjectExamsPath(subjectA), body)
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status = %d, want 422; body %s", name, rec.Code, rec.Body.String())
			continue
		}
		wantError(t, rec, 422, "validation_failed", "body", "json")
	}
}

func TestCreateExamBodyTooLargeAPI(t *testing.T) {
	e := newAPIEnv()
	body := `{"name":"` + strings.Repeat("a", testMaxBodyBytes) + `","questionCount":65,"timeLimitMin":130,"passPercent":72}`

	wantError(t, e.do(t, http.MethodPost, subjectExamsPath(subjectA), body), 413, "payload_too_large", "", "")
}

func TestCreateExamOnAMissingSubjectAPI(t *testing.T) { // S02-4
	e := newAPIEnv()

	for _, subjectID := range []string{deadSub, unknownID} {
		wantError(t, e.do(t, http.MethodPost, subjectExamsPath(subjectID), examBody("X")), 422, "validation_failed", "subjectId", "not_found")
	}
	wantError(t, e.do(t, http.MethodPost, subjectExamsPath("not-a-uuid"), examBody("X")), 404, "not_found", "", "")
}

func TestListSubjectExamsAPI(t *testing.T) { // slice 02 §1 list
	e := newAPIEnv()
	e.createExam(t, subjectA, "charlie")
	e.createExam(t, subjectA, "Bravo")
	e.createExam(t, subjectA, "alpha")
	e.createExam(t, subjectB, "Elsewhere")

	list := decodeList(t, e.do(t, http.MethodGet, subjectExamsPath(subjectA), ""))

	if len(list) != 3 || list[0]["name"] != "alpha" || list[1]["name"] != "Bravo" || list[2]["name"] != "charlie" {
		t.Fatalf("list = %v", list)
	}
	item := list[0]
	if item["questionTotal"] != float64(0) || item["version"] != float64(1) || item["subjectId"] != subjectA {
		t.Errorf("item = %v", item)
	}
	if _, present := item["lastSimulation"]; present {
		t.Errorf("lastSimulation must be absent, got %v", item["lastSimulation"])
	}
	if _, present := item["classifications"]; present {
		t.Error("list items are ExamSummary: no classifications")
	}
}

func TestListSubjectExamsEmptyAndMissingSubjectAPI(t *testing.T) {
	e := newAPIEnv()

	if list := decodeList(t, e.do(t, http.MethodGet, subjectExamsPath(subjectA), "")); list == nil || len(list) != 0 {
		t.Errorf("list = %v, want an empty array", list)
	}
	wantError(t, e.do(t, http.MethodGet, subjectExamsPath(deadSub), ""), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodGet, subjectExamsPath("nope"), ""), 404, "not_found", "", "")
}

func TestGetExamAPI(t *testing.T) {
	e := newAPIEnv()
	created := e.createExam(t, subjectA, "SAA-C03")

	rec := e.do(t, http.MethodGet, examPath(strings.ToUpper(created["id"].(string))), "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	data := decodeData(t, rec)
	if data["id"] != created["id"] || data["name"] != "SAA-C03" {
		t.Errorf("data = %v", data)
	}
	if classifications, ok := data["classifications"].([]any); !ok || len(classifications) != 0 {
		t.Errorf("classifications = %v, want an empty array", data["classifications"])
	}
	wantError(t, e.do(t, http.MethodGet, examPath(unknownID), ""), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodGet, examPath("nope"), ""), 404, "not_found", "", "")
}

func TestUpdateExamAPI(t *testing.T) { // S02-5
	e := newAPIEnv()
	created := e.createExam(t, subjectA, "SAA-C03")
	path := examPath(created["id"].(string))

	rec := e.do(t, http.MethodPatch, path, `{"version":1,"name":" SAA-C04 ","passPercent":75}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	data := decodeData(t, rec)
	if data["version"] != float64(2) || data["name"] != "SAA-C04" || data["passPercent"] != float64(75) ||
		data["questionCount"] != float64(65) || data["timeLimitMin"] != float64(130) {
		t.Errorf("data = %v", data)
	}
	if got := decodeData(t, e.do(t, http.MethodGet, path, "")); got["version"] != float64(2) {
		t.Errorf("stored version = %v, want 2", got["version"])
	}
}

func TestUpdateExamStaleVersionAPI(t *testing.T) { // S02-5
	e := newAPIEnv()
	created := e.createExam(t, subjectA, "SAA-C03")
	path := examPath(created["id"].(string))
	e.do(t, http.MethodPatch, path, `{"version":1,"passPercent":75}`)

	wantError(t, e.do(t, http.MethodPatch, path, `{"version":1,"passPercent":80}`), 409, "conflict", "version", "stale_version")
	if got := decodeData(t, e.do(t, http.MethodGet, path, "")); got["passPercent"] != float64(75) {
		t.Errorf("passPercent = %v; the stale edit must not write", got["passPercent"])
	}
}

func TestUpdateExamValidationAPI(t *testing.T) {
	e := newAPIEnv()
	created := e.createExam(t, subjectA, "SAA-C03")
	e.createExam(t, subjectA, "Other")
	path := examPath(created["id"].(string))
	tests := []struct {
		name, body string
		status     int
		code       string
		field      string
		rule       string
	}{
		{"missing version", `{"passPercent":75}`, 422, "validation_failed", "version", "required"},
		{"nothing to change", `{"version":1}`, 422, "validation_failed", "body", "required"},
		{"range", `{"version":1,"timeLimitMin":601}`, 422, "validation_failed", "timeLimitMin", "range"},
		{"empty name", `{"version":1,"name":"  "}`, 422, "validation_failed", "name", "required"},
		{"null name", `{"version":1,"name":null}`, 422, "validation_failed", "name", "required"},
		{"duplicate name", `{"version":1,"name":"other"}`, 409, "conflict", "name", "duplicate_name"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantError(t, e.do(t, http.MethodPatch, path, tc.body), tc.status, tc.code, tc.field, tc.rule)
		})
	}
	wantError(t, e.do(t, http.MethodPatch, examPath(unknownID), `{"version":1,"passPercent":75}`), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodPatch, examPath("nope"), `{"version":1,"passPercent":75}`), 404, "not_found", "", "")
	big := `{"version":1,"name":"` + strings.Repeat("a", testMaxBodyBytes) + `"}`
	wantError(t, e.do(t, http.MethodPatch, path, big), 413, "payload_too_large", "", "")
}

func TestDeleteExamAPI(t *testing.T) { // S02-6
	e := newAPIEnv()
	created := e.createExam(t, subjectA, "SAA-C03")
	path := examPath(created["id"].(string))

	if rec := e.do(t, http.MethodDelete, path, ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body %s", rec.Code, rec.Body.String())
	}

	wantError(t, e.do(t, http.MethodGet, path, ""), 404, "not_found", "", "")
	if list := decodeList(t, e.do(t, http.MethodGet, subjectExamsPath(subjectA), "")); len(list) != 0 {
		t.Errorf("list after delete = %v, want empty", list)
	}
	wantError(t, e.do(t, http.MethodPatch, path, `{"version":1,"passPercent":75}`), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodDelete, path, ""), 404, "not_found", "", "")
	e.createExam(t, subjectA, "saa-c03") // the name is reusable
}

func TestDeleteExamUnknownOrMalformedIdAPI(t *testing.T) {
	e := newAPIEnv()

	wantError(t, e.do(t, http.MethodDelete, examPath(unknownID), ""), 404, "not_found", "", "")
	wantError(t, e.do(t, http.MethodDelete, examPath("nope"), ""), 404, "not_found", "", "")
}
