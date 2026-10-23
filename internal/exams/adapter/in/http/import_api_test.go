package http

import (
	"bytes"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"slices"
	"strings"
	"testing"

	"github.com/afwilto/studies-mngr/internal/exams/app/usecase"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

const importCap = 12 << 20 // a little above the 10 MiB JSON limit, as MAX_IMPORT_BYTES sits above the zip limit

type importEnv struct {
	apiEnv
	bank   *testsupport.FakeQuestionBank
	examID string
}

func newImportAPIEnv(t *testing.T, cap int64) importEnv {
	t.Helper()
	subjects := testsupport.NewFakeSubjectDirectory(subjectA, subjectB)
	repo := testsupport.NewFakeExamRepo(subjects)
	bank := testsupport.NewFakeQuestionBank(repo)
	clock := &testsupport.StepClock{}
	ids := &testsupport.SeqIDGen{}
	deps := Deps{
		CreateExam:       usecase.NewCreateExamService(repo, subjects, clock, ids),
		ListSubjectExams: usecase.NewListSubjectExamsService(repo, subjects),
		GetExam:          usecase.NewGetExamService(repo),
		UpdateExam:       usecase.NewUpdateExamService(repo, bank, clock),
		DeleteExam:       usecase.NewDeleteExamService(repo, clock),
		ImportQuestions:  usecase.NewImportQuestionsService(bank, clock, ids),
		FileReader:       sharedReader(t),
	}
	env := apiEnv{router: httpx.NewServer(httpx.Config{MaxBodyBytes: testMaxBodyBytes, BodyLimits: BodyLimits(cap)}, Register(deps))}
	exam := env.createExam(t, subjectA, "SAA-C03")
	return importEnv{apiEnv: env, bank: bank, examID: exam["id"].(string)}
}

func importPath(examID, query string) string {
	p := "/api/v1/exams/" + examID + "/questions/import"
	if query != "" {
		p += "?" + query
	}
	return p
}

// post sends a multipart upload with one `file` part.
func (e importEnv) post(t *testing.T, path, filename string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
	h.Set("Content-Type", "application/octet-stream")
	part, err := w.CreatePart(h)
	if err != nil {
		t.Fatal(err)
	}
	part.Write(content)
	w.Close()
	return e.send(t, path, w.FormDataContentType(), body.Bytes())
}

func (e importEnv) send(t *testing.T, path, contentType string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	assertConformsToContract(t, http.MethodPost, strings.SplitN(path, "?", 2)[0], rec)
	return rec
}

func (e importEnv) importExample(t *testing.T, query, name string) *httptest.ResponseRecorder {
	t.Helper()
	return e.post(t, importPath(e.examID, query), name, example(t, name))
}

func report(t *testing.T, rec *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d; body %s", rec.Code, status, rec.Body.String())
	}
	return decodeData(t, rec)
}

func counts(r map[string]any) [4]float64 {
	c := r["counts"].(map[string]any)
	return [4]float64{c["create"].(float64), c["update"].(float64), c["unchanged"].(float64), c["invalid"].(float64)}
}

func reportErrors(r map[string]any) []string {
	var out []string
	for _, e := range r["errors"].([]any) {
		m := e.(map[string]any)
		out = append(out, m["path"].(string)+"|"+m["rule"].(string))
	}
	return out
}

func TestDryRunOfTheAwsExampleReturnsTheReportAndWritesNothing(t *testing.T) { // AC-1, S03-8, report shape
	e := newImportAPIEnv(t, importCap)

	rec := e.importExample(t, "dryRun=true", "aws-text.json")

	r := report(t, rec, http.StatusOK)
	if r["dryRun"] != true || r["applied"] != false || counts(r) != [4]float64{3, 0, 0, 0} {
		t.Errorf("report = %v", r)
	}
	if r["primary"] != "Dominio" {
		t.Errorf("primary = %v", r["primary"])
	}
	newClassifications := r["newClassifications"].([]any)
	if len(newClassifications) != 2 || newClassifications[0].(map[string]any)["name"] != "Dominio" || newClassifications[1].(map[string]any)["name"] != "Servicio" {
		t.Errorf("newClassifications = %v", newClassifications)
	}
	if len(r["newCategories"].([]any)) != 6 || len(r["preview"].([]any)) != 3 || len(r["weightsAfter"].([]any)) != 3 {
		t.Errorf("newCategories %d preview %d weightsAfter %d", len(r["newCategories"].([]any)), len(r["preview"].([]any)), len(r["weightsAfter"].([]any)))
	}
	media := r["media"].(map[string]any)
	if media["referenced"] != float64(0) || len(media["missing"].([]any)) != 0 {
		t.Errorf("media = %v", media)
	}
	exam := decodeData(t, e.do(t, http.MethodGet, examPath(e.examID), ""))
	if exam["questionTotal"] != float64(0) || len(exam["classifications"].([]any)) != 0 {
		t.Errorf("exam after a dry run = %v", exam)
	}
}

func TestDryRunIsTheOnlyModeThatNeedsTheFlagApplyIsTheDefault(t *testing.T) { // openapi: dryRun defaults to false
	e := newImportAPIEnv(t, importCap)

	r := report(t, e.importExample(t, "", "aws-text.json"), http.StatusOK)

	if r["dryRun"] != false || r["applied"] != true {
		t.Errorf("report = dryRun %v applied %v", r["dryRun"], r["applied"])
	}
}

func TestApplyThenGetExamShowsTheInferredTaxonomyAndWeights(t *testing.T) { // FR-3, slice 02 "filled in by slice 03"
	e := newImportAPIEnv(t, importCap)
	e.importExample(t, "dryRun=false", "aws-text.json")

	exam := decodeData(t, e.do(t, http.MethodGet, examPath(e.examID), ""))

	if exam["questionTotal"] != float64(3) {
		t.Errorf("questionTotal = %v", exam["questionTotal"])
	}
	classes := exam["classifications"].([]any)
	if len(classes) != 2 {
		t.Fatalf("classifications = %v", classes)
	}
	primary := classes[0].(map[string]any)
	if primary["name"] != "Dominio" || primary["position"] != float64(0) || primary["weightMode"] != "inferred" {
		t.Errorf("primary = %v", primary)
	}
	var weights []float64
	for _, c := range primary["categories"].([]any) {
		cat := c.(map[string]any)
		weights = append(weights, cat["weightPercent"].(float64))
		if cat["questionCount"] != float64(1) || cat["paceQuestions"] != nil || cat["paceMinutes"] != nil || cat["key"] == "" {
			t.Errorf("category = %v", cat)
		}
	}
	if !slices.Equal(weights, []float64{34, 33, 33}) {
		t.Errorf("weights = %v", weights)
	}
	secondary := classes[1].(map[string]any)
	if secondary["position"] != float64(1) || secondary["categories"].([]any)[0].(map[string]any)["weightPercent"] != nil {
		t.Errorf("secondary = %v: weightPercent is null outside the primary", secondary)
	}
	list := decodeList(t, e.do(t, http.MethodGet, subjectExamsPath(subjectA), ""))
	if list[0]["questionTotal"] != float64(3) {
		t.Errorf("list questionTotal = %v", list[0]["questionTotal"])
	}
}

func TestReimportReportsEverythingUnchanged(t *testing.T) { // AC-2
	e := newImportAPIEnv(t, importCap)
	e.importExample(t, "", "aws-text.json")

	r := report(t, e.importExample(t, "dryRun=true", "aws-text.json"), http.StatusOK)

	if counts(r) != [4]float64{0, 0, 3, 0} || len(r["newCategories"].([]any)) != 0 || len(r["duplicateStems"].([]any)) != 0 {
		t.Errorf("report = %v", r)
	}
}

func TestSchemaErrorsAre422WithTheFullReport(t *testing.T) { // slice 03 §1.3 stage 2, contract oneOf
	e := newImportAPIEnv(t, importCap)
	doc := mutate(t, func(d map[string]any) {
		question(d, 0)["key"] = "Bad Key"
		question(d, 2)["type"] = "bool"
	})

	r := report(t, e.post(t, importPath(e.examID, "dryRun=true"), "q.json", doc), http.StatusUnprocessableEntity)

	if r["applied"] != false || r["dryRun"] != true {
		t.Errorf("applied %v dryRun %v", r["applied"], r["dryRun"])
	}
	got := reportErrors(r)
	if !slices.Contains(got, "questions[0].key|schema") || !slices.Contains(got, "questions[2].type|schema") {
		t.Errorf("errors = %v", got)
	}
	if c := counts(r); c[3] != 2 || c[0] != 0 {
		t.Errorf("counts = %v, want 2 invalid", c)
	}
}

func TestInvalidJSONIs422WithRuleJsonAtTheRoot(t *testing.T) { // stage 1
	e := newImportAPIEnv(t, importCap)

	r := report(t, e.post(t, importPath(e.examID, ""), "q.json", []byte(`{"format": "afwilto-questions", `)), http.StatusUnprocessableEntity)

	if got := reportErrors(r); !slices.Equal(got, []string{"$|json"}) || r["applied"] != false {
		t.Errorf("errors = %v applied %v", got, r["applied"])
	}
}

func TestSemanticErrorsAre422AndNothingIsWritten(t *testing.T) { // S03-4 over HTTP, BR-I1
	e := newImportAPIEnv(t, importCap)
	doc := mutate(t, func(d map[string]any) {
		qs := d["questions"].([]any)
		var dup map[string]any
		b, _ := json.Marshal(qs[1])
		json.Unmarshal(b, &dup)
		d["questions"] = append(qs, dup) // a fourth question repeating the key of the second
	})

	r := report(t, e.post(t, importPath(e.examID, ""), "q.json", doc), http.StatusUnprocessableEntity)

	if got := reportErrors(r); !slices.Equal(got, []string{"questions[3].key|duplicate_key"}) {
		t.Errorf("errors = %v", got)
	}
	if len(e.bank.Bank(e.examID).Questions) != 0 {
		t.Error("an invalid import must write nothing")
	}
}

func TestPrimaryQueryParameterIsPassedThrough(t *testing.T) { // S03-6 over HTTP
	e := newImportAPIEnv(t, importCap)

	r := report(t, e.importExample(t, "dryRun=true&primary=Servicio", "aws-text.json"), http.StatusUnprocessableEntity)

	if got := reportErrors(r); !slices.Equal(got, []string{"questions[0].categories|primary_single_valued"}) {
		t.Errorf("errors = %v", got)
	}
}

func TestAnyMediaReferenceIs422MediaMissingInThisSlice(t *testing.T) { // stage 4, S03-1 exclusion
	e := newImportAPIEnv(t, importCap)

	r := report(t, e.importExample(t, "dryRun=true", "moto-images.json"), http.StatusUnprocessableEntity)

	media := r["media"].(map[string]any)
	if media["referenced"] != float64(3) || len(media["missing"].([]any)) != 3 || media["inZip"] != float64(0) {
		t.Errorf("media = %v", media)
	}
	for _, e := range reportErrors(r) {
		if !strings.HasSuffix(e, "|media_missing") {
			t.Errorf("error %s, want only media_missing", e)
		}
	}
}

func TestAZipUploadIsRejectedWithFileType(t *testing.T) { // S03-7
	e := newImportAPIEnv(t, importCap)
	zip := append([]byte("PK\x03\x04"), bytes.Repeat([]byte{0}, 64)...)

	rec := e.post(t, importPath(e.examID, "dryRun=true"), "questions.zip", zip)

	wantError(t, rec, http.StatusUnprocessableEntity, "validation_failed", "file", "file_type")
}

func TestOnlyAFileStartingWithABraceIsTakenAsJSON(t *testing.T) { // slice 03 §1.2: detected from the first non-space byte
	e := newImportAPIEnv(t, importCap)
	for name, content := range map[string][]byte{
		"an array":         []byte(`[{"key":"a"}]`),
		"plain text":       []byte("hello"),
		"empty":            {},
		"only whitespace":  []byte(" \n\t "),
		"a zip by content": []byte("PK\x03\x04rest"),
		"a PNG":            {0x89, 'P', 'N', 'G'},
	} {
		rec := e.post(t, importPath(e.examID, ""), "upload.json", content) // the name lies; the bytes decide
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d, body %s", name, rec.Code, rec.Body.String())
			continue
		}
		wantError(t, rec, http.StatusUnprocessableEntity, "validation_failed", "file", "file_type")
	}
}

func TestLeadingWhitespaceBeforeTheBraceIsFine(t *testing.T) {
	e := newImportAPIEnv(t, importCap)

	rec := e.post(t, importPath(e.examID, "dryRun=true"), "q.json", append([]byte("\n  \t"), example(t, "aws-text.json")...))

	report(t, rec, http.StatusOK)
}

func TestAJSONFileAboveTenMiBIs413(t *testing.T) { // S03-7
	e := newImportAPIEnv(t, importCap)
	big := append([]byte("{"), bytes.Repeat([]byte(" "), 10<<20)...) // 10 MiB + 1 byte in total

	rec := e.post(t, importPath(e.examID, "dryRun=true"), "big.json", big)

	decodeError(t, rec, http.StatusRequestEntityTooLarge, "payload_too_large")
}

func TestAJSONFileOfExactlyTenMiBIsNotTooLarge(t *testing.T) { // boundary
	e := newImportAPIEnv(t, importCap)
	content := append([]byte("{"), bytes.Repeat([]byte(" "), 10<<20-1)...)

	rec := e.post(t, importPath(e.examID, "dryRun=true"), "edge.json", content)

	if rec.Code == http.StatusRequestEntityTooLarge {
		t.Error("exactly 10 MiB must be accepted as far as size goes")
	}
	report(t, rec, http.StatusUnprocessableEntity) // an unterminated object: stage 1, rule json
}

func TestABodyAboveTheRouteCapIs413(t *testing.T) { // S01-5 cap still applies to the route
	e := newImportAPIEnv(t, 2000)

	rec := e.post(t, importPath(e.examID, ""), "q.json", example(t, "aws-text.json"))

	decodeError(t, rec, http.StatusRequestEntityTooLarge, "payload_too_large")
}

func TestMissingFilePartOrNotMultipartIs422OnFile(t *testing.T) {
	e := newImportAPIEnv(t, importCap)

	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	w.WriteField("other", "x")
	w.Close()
	cases := map[string]*httptest.ResponseRecorder{
		"no file part": e.send(t, importPath(e.examID, ""), w.FormDataContentType(), body.Bytes()),
		"json body":    e.send(t, importPath(e.examID, ""), "application/json", example(t, "aws-text.json")),
		"empty body":   e.send(t, importPath(e.examID, ""), "multipart/form-data; boundary=x", nil),
	}
	for name, rec := range cases {
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d body %s", name, rec.Code, rec.Body.String())
			continue
		}
		wantError(t, rec, http.StatusUnprocessableEntity, "validation_failed", "file", "required")
	}
}

func TestQueryParametersAreValidated(t *testing.T) {
	e := newImportAPIEnv(t, importCap)
	for name, tc := range map[string]struct{ query, field, rule string }{
		"dryRun not a boolean": {"dryRun=maybe", "dryRun", "format"},
		"dryRun 1":             {"dryRun=1", "dryRun", "format"},
		"empty primary":        {"primary=", "primary", "required"},
		"blank primary":        {"primary=%20%20", "primary", "required"},
		"primary too long":     {"primary=" + strings.Repeat("a", 41), "primary", "max_length"},
	} {
		rec := e.importExample(t, tc.query, "aws-text.json")
		if rec.Code != http.StatusUnprocessableEntity {
			t.Errorf("%s: status %d", name, rec.Code)
			continue
		}
		wantError(t, rec, http.StatusUnprocessableEntity, "validation_failed", tc.field, tc.rule)
	}
	// accepted spellings
	report(t, e.importExample(t, "dryRun=false&primary=Dominio", "aws-text.json"), http.StatusOK)
}

func TestImportIntoAnUnknownExamIs404(t *testing.T) {
	e := newImportAPIEnv(t, importCap)

	for name, id := range map[string]string{"unknown": unknownID, "not a uuid": "nope"} {
		rec := e.post(t, importPath(id, ""), "q.json", example(t, "aws-text.json"))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
}

func TestDeletingAnExamWithQuestionsIs409InUse(t *testing.T) { // S03-9
	e := newImportAPIEnv(t, importCap)
	e.importExample(t, "", "aws-text.json")

	wantError(t, e.do(t, http.MethodDelete, examPath(e.examID), ""), http.StatusConflict, "conflict", "exam", "in_use")

	if rec := e.do(t, http.MethodGet, examPath(e.examID), ""); rec.Code != http.StatusOK {
		t.Errorf("the exam must still exist: %d", rec.Code)
	}
}

func TestRouteExistsOnlyWhenTheImportDependenciesAreWired(t *testing.T) { // Register(Deps{}) keeps the slice 02 shape
	withDeps := http.NewServeMux()
	Register(Deps{ImportQuestions: usecase.NewImportQuestionsService(nil, nil, nil), FileReader: sharedReader(t)})(withDeps)
	without := http.NewServeMux()
	Register(Deps{})(without)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/exams/"+unknownID+"/questions/import", nil)

	if _, pattern := withDeps.Handler(req); pattern != ImportPattern {
		t.Errorf("wired: pattern %q, want %q", pattern, ImportPattern)
	}
	if _, pattern := without.Handler(req); pattern != "" {
		t.Errorf("unwired: pattern %q, want none", pattern)
	}
}
