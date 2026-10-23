package http

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
)

// Spec 18 slice 01 (S01-1): the v0.2.0 contract documents the whole §6 API so
// slices 02–06b implement against a fixed artifact.

// operationOf returns nil instead of panicking when the contract lacks the
// path, so a missing endpoint fails its test rather than the whole package.
func operationOf(doc *openapi3.T, method, path string) *openapi3.Operation {
	item := doc.Paths.Find(path)
	if item == nil {
		return nil
	}
	return item.GetOperation(method)
}

func TestContractIsV030(t *testing.T) { // S01-1, slice 06b decision 5
	doc, _ := loadContract(t)
	if got := doc.Info.Version; got != "0.3.0" {
		t.Errorf("info.version = %q, want 0.3.0", got)
	}
}

func TestContractCarriesTheSlice06bAdditions(t *testing.T) { // slice 06b decisions 1 and 2
	doc, _ := loadContract(t)
	attempt := doc.Components.Schemas["Attempt"]
	if attempt == nil || attempt.Value.Properties["result"] == nil {
		t.Error("Attempt has no optional result property")
	} else if slices.Contains(attempt.Value.Required, "result") {
		t.Error("Attempt.result must be optional: only a closed attempt carries it")
	}
	answer := doc.Components.Schemas["AnswerResponse"]
	if answer == nil || answer.Value.Properties["data"].Value.Properties["remainingSeconds"] == nil {
		t.Error("AnswerResponse.data has no remainingSeconds property")
	} else if slices.Contains(answer.Value.Properties["data"].Value.Required, "remainingSeconds") {
		t.Error("AnswerResponse.data.remainingSeconds must be optional: untimed attempts omit it")
	}
}

func TestContractDocumentsEveryExamsEndpoint(t *testing.T) { // S01-1
	doc, _ := loadContract(t)
	want := []string{
		"GET /api/v1/subjects/{id}/exams",
		"POST /api/v1/subjects/{id}/exams",
		"GET /api/v1/exams/{id}",
		"PATCH /api/v1/exams/{id}",
		"DELETE /api/v1/exams/{id}",
		"POST /api/v1/exams/{id}/categories/merge",
		"POST /api/v1/exams/{id}/questions/import",
		"GET /api/v1/exams/{id}/questions/export",
		"GET /api/v1/exams/{id}/questions",
		"GET /api/v1/questions/{id}",
		"DELETE /api/v1/questions/{id}",
		"GET /api/v1/media/{sha256}",
		"POST /api/v1/exams/{id}/sessions/preview",
		"POST /api/v1/exams/{id}/attempts",
		"GET /api/v1/attempts/{id}",
		"PUT /api/v1/attempts/{id}/items/{position}",
		"POST /api/v1/attempts/{id}/submit",
		"GET /api/v1/attempts/{id}/review",
		"GET /api/v1/exams/{id}/attempts",
	}
	for _, endpoint := range want {
		method, path, _ := strings.Cut(endpoint, " ")
		item := doc.Paths.Find(path)
		if item == nil || item.GetOperation(method) == nil {
			t.Errorf("contract is missing %s", endpoint)
		}
	}
}

func TestContractDocumentsExamsSchemas(t *testing.T) { // S01-1
	doc, _ := loadContract(t)
	// `ExamCategory` rather than `Category`: the latter is the studies tree
	// schema (slice 01 §8, amendment).
	want := []string{
		"Exam", "ExamSummary", "Classification", "ExamCategory", "QuestionSummary", "Question",
		"QuestionOption", "ImportReport", "SessionConfig", "SessionPreview", "Attempt",
		"AttemptItem", "AttemptResult", "ReviewItem", "AttemptSummary", "ErrorDetail",
	}
	for _, name := range want {
		if doc.Components.Schemas[name] == nil {
			t.Errorf("components.schemas is missing %s", name)
		}
	}
}

// The closed set of new error rules of Spec 18 §6, plus the BR-E1/E3 and
// stale-version rules the exams API reuses.
var examsErrorRules = []string{
	"duplicate_name", "in_use", "stale_version",
	"range",
	"json", "schema", "file_type", "too_many_classifications", "primary_single_valued",
	"correct_count", "option_content", "duplicate_key", "pattern", "media_missing",
	"weights_sum", "pace_pair", "merge_scope",
	"zip_entries", "zip_size", "zip_path", "zip_layout", "media_type", "media_size", "media_dimensions",
	"insufficient_questions", "locked_by_simulation", "attempt_in_progress",
	"attempt_expired", "attempt_closed", "too_many_selected", "invalid_label", "answer_locked",
}

func TestErrorDetailRuleDescriptionListsEveryExamsRule(t *testing.T) { // S01-1
	doc, _ := loadContract(t)
	detail := doc.Components.Schemas["ErrorDetail"]
	if detail == nil || detail.Value == nil || detail.Value.Properties["rule"] == nil {
		t.Fatal("components.schemas.ErrorDetail.properties.rule is missing")
	}
	description := detail.Value.Properties["rule"].Value.Description
	for _, rule := range examsErrorRules {
		if !containsWord(description, rule) {
			t.Errorf("ErrorDetail.rule description does not list %q", rule)
		}
	}
}

func containsWord(text, word string) bool {
	for _, field := range strings.FieldsFunc(text, func(r rune) bool {
		return !(r == '_' || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if field == word {
			return true
		}
	}
	return false
}

func TestErrorResponseReusesErrorDetail(t *testing.T) { // S01-1
	doc, _ := loadContract(t)
	errResp := doc.Components.Schemas["ErrorResponse"]
	details := errResp.Value.Properties["error"].Value.Properties["details"]
	if details == nil || details.Value.Items == nil || details.Value.Items.Ref != "#/components/schemas/ErrorDetail" {
		t.Errorf("ErrorResponse.error.details.items must $ref ErrorDetail, got %+v", details)
	}
}

func TestImportEndpointIsMultipart(t *testing.T) { // S01-1
	doc, _ := loadContract(t)
	op := operationOf(doc, http.MethodPost, "/api/v1/exams/{id}/questions/import")
	if op == nil {
		t.Fatal("import operation missing")
	}
	params := map[string]*openapi3.Parameter{}
	for _, p := range op.Parameters {
		params[p.Value.Name] = p.Value
	}
	if p := params["dryRun"]; p == nil || p.In != "query" || !p.Schema.Value.Type.Is("boolean") {
		t.Errorf("dryRun must be a boolean query parameter, got %+v", p)
	}
	if p := params["primary"]; p == nil || p.In != "query" || !p.Schema.Value.Type.Is("string") {
		t.Errorf("primary must be a string query parameter, got %+v", p)
	}
	if op.RequestBody == nil {
		t.Fatal("import has no request body")
	}
	media := op.RequestBody.Value.Content.Get("multipart/form-data")
	if media == nil {
		t.Fatal("import request body must be multipart/form-data")
	}
	file := media.Schema.Value.Properties["file"]
	if file == nil || !file.Value.Type.Is("string") || file.Value.Format != "binary" {
		t.Errorf("multipart schema needs file: string/binary, got %+v", file)
	}
	if !slices.Contains(media.Schema.Value.Required, "file") {
		t.Error("multipart file must be required")
	}
	ok := op.Responses.Status(200)
	if ok == nil || ok.Value.Content.Get("application/json") == nil {
		t.Error("import must document a 200 JSON report")
	}
	unprocessable := op.Responses.Status(422)
	if unprocessable == nil {
		t.Fatal("import must document a 422 (report or envelope)")
	}
	if schema := unprocessable.Value.Content.Get("application/json").Schema.Value; len(schema.OneOf) != 2 {
		t.Errorf("422 must be oneOf [ErrorResponse, ImportReportResponse], got %d alternatives", len(schema.OneOf))
	}
	if op.Responses.Status(413) == nil {
		t.Error("import must document 413")
	}
}

func TestImportReportShape(t *testing.T) { // S01-1, parent FR-4
	doc, _ := loadContract(t)
	report := doc.Components.Schemas["ImportReport"]
	if report == nil {
		t.Fatal("ImportReport missing")
	}
	for _, field := range []string{
		"dryRun", "applied", "counts", "keys", "errors", "primary", "newClassifications",
		"newCategories", "weightsAfter", "duplicateStems", "media", "preview", "warnings",
	} {
		if report.Value.Properties[field] == nil {
			t.Errorf("ImportReport lacks property %s", field)
		}
		if !slices.Contains(report.Value.Required, field) {
			t.Errorf("ImportReport.%s must be required", field)
		}
	}
}

func TestMediaAndExportResponses(t *testing.T) { // S01-1, parent FR-5/FR-6
	doc, _ := loadContract(t)

	media := operationOf(doc, http.MethodGet, "/api/v1/media/{sha256}")
	if media == nil {
		t.Fatal("media operation missing")
	}
	ok := media.Responses.Status(200).Value
	for _, contentType := range []string{"image/png", "image/jpeg", "image/webp"} {
		if ok.Content.Get(contentType) == nil {
			t.Errorf("GET /media/{sha256} 200 must serve %s", contentType)
		}
	}
	if ok.Headers["Cache-Control"] == nil || ok.Headers["X-Content-Type-Options"] == nil {
		t.Errorf("GET /media/{sha256} 200 must document Cache-Control and X-Content-Type-Options, got %v", ok.Headers)
	}
	if media.Responses.Status(404) == nil {
		t.Error("GET /media/{sha256} must document 404")
	}

	export := operationOf(doc, http.MethodGet, "/api/v1/exams/{id}/questions/export")
	if export == nil {
		t.Fatal("export operation missing")
	}
	if export.Responses.Status(200).Value.Content.Get("application/zip") == nil {
		t.Error("export 200 must serve application/zip")
	}
}

// S01-7: documented but unregistered → the JSON 404 envelope, and that
// envelope conforms to the contract's 404 response.
func TestUnregisteredExamsRouteAnswersContractNotFound(t *testing.T) {
	path := "/api/v1/subjects/0192f3a4-0000-7000-8000-000000000001/exams"
	rec := serve(newTestRouter(nil), httptest.NewRequest(http.MethodGet, path, nil))

	wantError(t, rec, http.StatusNotFound, "not_found", "", "")
	assertConformsToContract(t, http.MethodGet, path, rec)
}

const (
	idA = "0192f3a4-0000-7000-8000-00000000000a"
	idB = "0192f3a4-0000-7000-8000-00000000000b"
	idC = "0192f3a4-0000-7000-8000-00000000000c"
	idD = "0192f3a4-0000-7000-8000-00000000000d"

	examJSON = `{"id":"` + idA + `","subjectId":"` + idB + `","name":"SAA-C03","questionCount":65,` +
		`"timeLimitMin":130,"passPercent":72,"questionTotal":3,` +
		`"lastSimulation":{"scorePercent":71.5,"passed":false,"at":"2026-10-22T10:00:00Z"},` +
		`"version":2,"createdAt":"2026-10-22T09:00:00Z","updatedAt":"2026-10-22T09:30:00Z",` +
		`"classifications":[{"id":"` + idC + `","name":"Dominio","position":0,"weightMode":"inferred","categories":[` +
		`{"id":"` + idD + `","key":"alto-rendimiento","name":"Alto rendimiento","weightPercent":46,` +
		`"paceQuestions":10,"paceMinutes":20,"questionCount":5},` +
		`{"id":"` + idB + `","key":"seguridad","name":"Seguridad","weightPercent":null,` +
		`"paceQuestions":null,"paceMinutes":null,"questionCount":0}]}]}`

	questionJSON = `{"id":"` + idA + `","examId":"` + idB + `","key":"td-1","type":"multi","stem":"Elige **dos**",` +
		`"explanation":null,"difficulty":"hard","source":{"kind":"purchased","name":"TD Timed Set 3"},` +
		`"origin":{"set":"TD Timed Set 3","number":4,"noteRef":"03 · Route 53 (diap. 12)"},` +
		`"categories":[{"classification":"Dominio","name":"Seguridad"}],` +
		`"options":[{"position":0,"label":"A","text":"uno","correct":true,"rationale":"porque"},` +
		`{"position":1,"label":"B","image":"media:` + sha + `","alt":"señal","correct":true},` +
		`{"position":2,"label":"C","text":"tres","correct":false}],` +
		`"version":1,"createdAt":"2026-10-22T09:00:00Z","updatedAt":"2026-10-22T09:00:00Z"}`

	sha = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

	reportJSON = `{"dryRun":true,"applied":false,"counts":{"create":3,"update":0,"unchanged":0,"invalid":0},` +
		`"keys":{"create":["a","b","c"],"update":[],"unchanged":[]},"errors":[],"primary":"Dominio",` +
		`"newClassifications":[{"name":"Dominio"},{"name":"Servicio"}],` +
		`"newCategories":[{"classification":"Dominio","name":"Alto rendimeinto","similarTo":["Alto rendimiento"]}],` +
		`"weightsAfter":[{"category":"Alto rendimiento","percent":46,"mode":"inferred"}],` +
		`"duplicateStems":[{"key":"td-1","existingKey":"wz-1"}],` +
		`"media":{"referenced":0,"inZip":0,"missing":[],"unused":[]},"preview":[` + questionJSON + `],"warnings":["x"]}`

	attemptJSON = `{"id":"` + idA + `","examId":"` + idB + `","config":{"selection":{"kind":"categories",` +
		`"classificationId":"` + idC + `","categoryIds":["` + idD + `"],"count":"all"},` +
		`"timing":{"kind":"timed","minutes":30},"feedback":"each"},"status":"in_progress",` +
		`"startedAt":"2026-10-22T09:00:00Z","deadlineAt":"2026-10-22T09:30:00Z","remainingSeconds":1800,` +
		`"suggestedMinutes":30,"items":[{"position":0,"stem":"s","type":"multi","chooseCount":2,` +
		`"options":[{"label":"A","text":"x"},{"label":"B","image":"media:` + sha + `","alt":"a"},{"label":"C","text":"z"}],` +
		`"selected":["A"],"flagged":false,"answered":true,"correct":false,"correctLabels":["B","C"],` +
		`"rationales":{"B":"porque"},"explanation":"e"}]}`
)

// The contract must accept the payload shapes the slices will produce and
// reject the ones they must never produce; otherwise later conformance
// assertions are vacuous.
func TestContractAcceptsAndRejectsExamsPayloads(t *testing.T) { // S01-1
	const json = "application/json"
	cases := []struct {
		name, method, path string
		status             int
		contentType, body  string
		valid              bool
	}{
		{"exam detail", "GET", "/api/v1/exams/" + idA, 200, json, `{"data":` + examJSON + `}`, true},
		{"exam without classifications", "GET", "/api/v1/exams/" + idA, 200, json,
			`{"data":` + strings.Replace(examJSON, `"classifications":[`, `"classificationz":[`, 1) + `}`, false},
		{"exam list", "GET", "/api/v1/subjects/" + idA + "/exams", 200, json,
			`{"data":[{"id":"` + idA + `","subjectId":"` + idB + `","name":"E","questionCount":1,"timeLimitMin":1,` +
				`"passPercent":1,"questionTotal":0,"version":1,"createdAt":"2026-10-22T09:00:00Z","updatedAt":"2026-10-22T09:00:00Z"}],` +
				`"meta":{"totalItems":1}}`, true},
		{"exam created", "POST", "/api/v1/subjects/" + idA + "/exams", 201, json, `{"data":` + examJSON + `}`, true},
		{"exam deleted", "DELETE", "/api/v1/exams/" + idA, 204, "", ``, true},
		{"merge returns the exam", "POST", "/api/v1/exams/" + idA + "/categories/merge", 200, json, `{"data":` + examJSON + `}`, true},
		{"question list", "GET", "/api/v1/exams/" + idA + "/questions", 200, json,
			`{"data":[{"id":"` + idA + `","key":"k","type":"single","stemExcerpt":"hola","categories":[{"classification":"Dominio","name":"X"}],` +
				`"difficulty":null,"origin":null,"version":1,"updatedAt":"2026-10-22T09:00:00Z"}],"meta":{"totalItems":1}}`, true},
		{"question", "GET", "/api/v1/questions/" + idA, 200, json, `{"data":` + questionJSON + `}`, true},
		{"question with a bad type", "GET", "/api/v1/questions/" + idA, 200, json,
			`{"data":` + strings.Replace(questionJSON, `"type":"multi"`, `"type":"ordering"`, 1) + `}`, false},
		{"import dry run", "POST", "/api/v1/exams/" + idA + "/questions/import", 200, json, `{"data":` + reportJSON + `}`, true},
		{"import rejected with a report", "POST", "/api/v1/exams/" + idA + "/questions/import", 422, json, `{"data":` +
			strings.Replace(reportJSON, `"errors":[]`, `"errors":[{"path":"questions[3].options","rule":"correct_count","message":"m"}]`, 1) + `}`, true},
		{"import rejected with an envelope", "POST", "/api/v1/exams/" + idA + "/questions/import", 422, json,
			`{"error":{"code":"validation_failed","message":"m","details":[{"field":"file","rule":"file_type","message":"m"}]}}`, true},
		{"preview", "POST", "/api/v1/exams/" + idA + "/sessions/preview", 200, json,
			`{"data":{"poolSize":84,"drawCount":65,"suggestedMinutes":130,"shortages":[{"category":"Seguridad","needed":20,"available":12}]}}`, true},
		{"attempt started", "POST", "/api/v1/exams/" + idA + "/attempts", 201, json, `{"data":` + attemptJSON + `}`, true},
		{"attempt with an unknown status", "GET", "/api/v1/attempts/" + idA, 200, json,
			`{"data":` + strings.Replace(attemptJSON, `"in_progress"`, `"paused"`, 1) + `}`, false},
		{"answer under end feedback", "PUT", "/api/v1/attempts/" + idA + "/items/0", 200, json,
			`{"data":{"position":0,"selected":["A"],"flagged":true}}`, true},
		{"answer revealed under each feedback", "PUT", "/api/v1/attempts/" + idA + "/items/0", 200, json,
			`{"data":{"position":0,"selected":["B"],"flagged":false,"correct":false,"correctLabels":["C"],"rationales":{"C":"r"},"explanation":"e"}}`, true},
		{"answer missing flagged", "PUT", "/api/v1/attempts/" + idA + "/items/0", 200, json,
			`{"data":{"position":0,"selected":["A"]}}`, false},
		{"insufficient questions carries the shortage", "POST", "/api/v1/exams/" + idA + "/attempts", 422, json,
			`{"error":{"code":"validation_failed","message":"m","details":[{"field":"config.selection","rule":"insufficient_questions","message":"m","category":"Seguridad","needed":20,"available":12}]}}`, true},
		{"attempt in progress names the attempt", "POST", "/api/v1/exams/" + idA + "/attempts", 409, json,
			`{"error":{"code":"conflict","message":"m","details":[{"field":"attemptId","rule":"attempt_in_progress","message":"` + idA + `"}]}}`, true},
		{"result", "POST", "/api/v1/attempts/" + idA + "/submit", 200, json,
			`{"data":{"scorePercent":50,"passed":null,"correctCount":2,"total":4,"durationSeconds":600,"breakdown":[` +
				`{"classification":"Dominio","categories":[{"name":"B","correct":0,"total":2,"percent":0},{"name":"A","correct":2,"total":2,"percent":100}]}]}}`, true},
		{"review", "GET", "/api/v1/attempts/" + idA + "/review", 200, json,
			`{"data":[{"position":0,"stem":"s","type":"single","options":[{"label":"A","text":"x"},{"label":"B","text":"y"}],` +
				`"selected":["A"],"flagged":false,"correct":false,"correctLabels":["B"],"rationales":{"B":"r"},"explanation":"e",` +
				`"categories":[{"classification":"Dominio","name":"A"}],"noteId":"` + idC + `"}],"meta":{"totalItems":1}}`, true},
		{"history with trend", "GET", "/api/v1/exams/" + idA + "/attempts", 200, json,
			`{"data":[{"id":"` + idA + `","selectionKind":"simulation","timingKind":"timed","feedback":"end","status":"submitted",` +
				`"startedAt":"2026-10-22T09:00:00Z","durationSeconds":600,"scorePercent":80,"passed":true}],` +
				`"meta":{"totalItems":1,"trend":[{"at":"2026-10-22T09:10:00Z","scorePercent":80}]}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			if tc.contentType != "" {
				rec.Header().Set("Content-Type", tc.contentType)
			}
			rec.WriteHeader(tc.status)
			rec.Body.WriteString(tc.body)

			err := contractViolation(t, tc.method, tc.path, rec)
			if tc.valid && err != nil {
				t.Errorf("contract rejects a payload it must accept: %v", err)
			}
			if !tc.valid && err == nil {
				t.Error("contract accepts a payload it must reject")
			}
		})
	}
}

func TestContractServesMediaAndExportBytes(t *testing.T) { // S01-1
	for _, tc := range []struct {
		name, path, contentType string
	}{
		{"png", "/api/v1/media/" + sha, "image/png"},
		{"jpeg", "/api/v1/media/" + sha, "image/jpeg"},
		{"webp", "/api/v1/media/" + sha, "image/webp"},
		{"zip", "/api/v1/exams/" + idA + "/questions/export", "application/zip"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			rec.Header().Set("Content-Type", tc.contentType)
			rec.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			rec.Header().Set("X-Content-Type-Options", "nosniff")
			rec.WriteHeader(200)
			rec.Body.Write([]byte{0x89, 'P', 'N', 'G', 0x00, 0x01})
			if err := contractViolation(t, "GET", tc.path, rec); err != nil {
				t.Errorf("contract rejects %s bytes: %v", tc.contentType, err)
			}
		})
	}
}
