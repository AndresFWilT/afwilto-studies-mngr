package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/usecase"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

// bankAPI is the whole slice 04 surface over fakes: an exam with the
// six-question fixture of S04-1, imported through the use case.
type bankAPI struct {
	apiEnv
	examID string
}

func apiQuestion(key string, set string, number int, difficulty string, categories ...domain.ClassifiedNames) domain.FileQuestion {
	q := domain.FileQuestion{
		Key: key, Type: domain.QuestionSingle, Stem: "Stem of " + key, Difficulty: difficulty, Categories: categories,
		Options: []domain.Option{{Text: "wrong one"}, {Text: "right one", Correct: true, Rationale: "why"}, {Text: "wrong two"}},
	}
	if set != "" {
		q.Origin = &domain.Origin{Set: set, Number: number}
	}
	return q
}

func classified(classification string, names ...string) domain.ClassifiedNames {
	return domain.ClassifiedNames{Classification: classification, Names: names}
}

// newBankDeps wires every exams use case over fakes.
func newBankDeps(t *testing.T) Deps {
	t.Helper()
	deps, _, _ := newBankDepsWith()
	return deps
}

func newBankDepsWith() (Deps, in.ImportQuestions, *testsupport.StepClock) {
	subjects := testsupport.NewFakeSubjectDirectory(subjectA, subjectB)
	repo := testsupport.NewFakeExamRepo(subjects)
	bank := testsupport.NewFakeQuestionBank(repo)
	clock := &testsupport.StepClock{}
	ids := &testsupport.SeqIDGen{}
	deps := Deps{
		CreateExam:        usecase.NewCreateExamService(repo, subjects, clock, ids),
		ListSubjectExams:  usecase.NewListSubjectExamsService(repo, subjects),
		GetExam:           usecase.NewGetExamService(repo),
		UpdateExam:        usecase.NewUpdateExamService(repo, bank, clock),
		DeleteExam:        usecase.NewDeleteExamService(repo, clock),
		ListExamQuestions: usecase.NewListExamQuestionsService(bank),
		GetQuestion:       usecase.NewGetQuestionService(bank),
		DeleteQuestion:    usecase.NewDeleteQuestionService(bank, clock),
		MergeCategories:   usecase.NewMergeCategoriesService(repo, bank, clock),
	}
	return deps, usecase.NewImportQuestionsService(bank, clock, ids), clock
}

func newBankAPI(t *testing.T, questions ...domain.FileQuestion) bankAPI {
	t.Helper()
	deps, importer, _ := newBankDepsWith()
	env := apiEnv{router: httpx.NewServer(httpx.Config{MaxBodyBytes: 1 << 20}, Register(deps))}
	exam := env.createExam(t, subjectA, "SAA-C03")
	b := bankAPI{apiEnv: env, examID: exam["id"].(string)}

	if len(questions) == 0 {
		questions = bankAPIFixture()
	}
	file := &domain.QuestionFile{Source: &domain.Source{Kind: "own", Name: "fixture"}, Questions: questions}
	report, err := importer.Execute(context.Background(), in.ImportQuestionsCommand{ExamID: b.examID, Primary: "Dominio", File: file})
	if err != nil || !report.Applied {
		t.Fatalf("seeding the bank: %v %+v", err, report.Errors)
	}
	return b
}

// bankAPIFixture: Dominio is the primary; VPC has 3 questions, Seguridad 2 and
// one question has no Dominio category.
func bankAPIFixture() []domain.FileQuestion {
	return []domain.FileQuestion{
		apiQuestion("vpc-1", "Timed Set 1", 1, "easy", classified("Dominio", "VPC")),
		apiQuestion("vpc-2", "Timed Set 3", 2, "hard", classified("Dominio", "VPC")),
		apiQuestion("vpc-3", "Timed Set 3", 1, "hard", classified("Dominio", "VPC")),
		apiQuestion("seg-1", "Review Set", 5, "easy", classified("Dominio", "Seguridad")),
		apiQuestion("seg-2", "", 0, "", classified("Dominio", "Seguridad")),
		apiQuestion("orphan", "", 0, "", classified("Servicio", "IAM")),
	}
}

func (b bankAPI) questionsPath(query string) string {
	p := examPath(b.examID) + "/questions"
	if query != "" {
		p += "?" + query
	}
	return p
}

func (b bankAPI) exam(t *testing.T) map[string]any {
	t.Helper()
	rec := b.do(t, http.MethodGet, examPath(b.examID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("get exam: %d %s", rec.Code, rec.Body)
	}
	return decodeData(t, rec)
}

func (b bankAPI) classification(t *testing.T, name string) map[string]any {
	t.Helper()
	for _, c := range b.exam(t)["classifications"].([]any) {
		if m := c.(map[string]any); m["name"] == name {
			return m
		}
	}
	t.Fatalf("no classification %q", name)
	return nil
}

func (b bankAPI) categoryID(t *testing.T, classification, name string) string {
	t.Helper()
	for _, k := range b.classification(t, classification)["categories"].([]any) {
		if m := k.(map[string]any); m["name"] == name {
			return m["id"].(string)
		}
	}
	t.Fatalf("no category %s/%s", classification, name)
	return ""
}

func (b bankAPI) keys(t *testing.T, query string) []string {
	t.Helper()
	rec := b.do(t, http.MethodGet, b.questionsPath(query), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("list %q: %d %s", query, rec.Code, rec.Body)
	}
	out := []string{}
	for _, row := range decodeList(t, rec) {
		out = append(out, row["key"].(string))
	}
	return out
}

func (b bankAPI) questionID(t *testing.T, key string) string {
	t.Helper()
	rec := b.do(t, http.MethodGet, b.questionsPath(""), "")
	for _, row := range decodeList(t, rec) {
		if row["key"] == key {
			return row["id"].(string)
		}
	}
	t.Fatalf("no question %q", key)
	return ""
}

func (b bankAPI) patch(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return b.do(t, http.MethodPatch, examPath(b.examID), body)
}

func (b bankAPI) version(t *testing.T) int { return int(b.exam(t)["version"].(float64)) }

// ── the bank browser ───────────────────────────────────────────────────────

func TestListQuestionsFiltersOverHTTP(t *testing.T) { // S04-1
	b := newBankAPI(t)
	vpc := b.categoryID(t, "Dominio", "VPC")
	cases := []struct {
		name  string
		query string
		want  []string
	}{
		{"every question, ordered by origin set, number and key", "", []string{"seg-1", "vpc-1", "vpc-3", "vpc-2", "orphan", "seg-2"}},
		{"category=VPC", "category=" + vpc, []string{"vpc-1", "vpc-3", "vpc-2"}},
		{"origin=Timed Set 3 by number", "origin=" + url.QueryEscape("Timed Set 3"), []string{"vpc-3", "vpc-2"}},
		{"unclassified=true", "unclassified=true", []string{"orphan"}},
		{"unclassified=false is no filter", "unclassified=false", []string{"seg-1", "vpc-1", "vpc-3", "vpc-2", "orphan", "seg-2"}},
		{"difficulty and category combine with AND", "difficulty=hard&category=" + vpc, []string{"vpc-3", "vpc-2"}},
		{"q ignores case", "q=" + url.QueryEscape("STEM OF SEG"), []string{"seg-1", "seg-2"}},
		{"an empty q is no filter", "q=", []string{"seg-1", "vpc-1", "vpc-3", "vpc-2", "orphan", "seg-2"}},
		{"an upper-case category id still matches", "category=" + strings.ToUpper(vpc), []string{"vpc-1", "vpc-3", "vpc-2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.keys(t, tc.query); !slices.Equal(got, tc.want) {
				t.Errorf("keys = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListQuestionsRowShapeAndTotal(t *testing.T) {
	b := newBankAPI(t)

	rec := b.do(t, http.MethodGet, b.questionsPath("origin=Review+Set"), "")

	rows := decodeList(t, rec) // asserts meta.totalItems == len(data)
	if len(rows) != 1 {
		t.Fatalf("rows = %v", rows)
	}
	r := rows[0]
	origin := r["origin"].(map[string]any)
	cats := r["categories"].([]any)[0].(map[string]any)
	if r["key"] != "seg-1" || r["type"] != "single" || r["stemExcerpt"] != "Stem of seg-1" || r["difficulty"] != "easy" ||
		origin["set"] != "Review Set" || origin["number"] != float64(5) || r["version"] != float64(1) || r["updatedAt"] == nil ||
		cats["classification"] != "Dominio" || cats["name"] != "Seguridad" {
		t.Errorf("row = %v", r)
	}
	if _, has := r["options"]; has {
		t.Error("a summary carries no options")
	}

	orphan := decodeList(t, b.do(t, http.MethodGet, b.questionsPath("unclassified=true"), ""))[0]
	if orphan["difficulty"] != nil || orphan["origin"] != nil {
		t.Errorf("absent difficulty and origin must be null: %v", orphan)
	}
}

func TestListQuestionsRejectsBadParametersTogether(t *testing.T) {
	b := newBankAPI(t)

	for query, field := range map[string]string{
		"difficulty=impossible": "difficulty",
		"category=not-a-uuid":   "category",
		"unclassified=maybe":    "unclassified",
	} {
		wantError(t, b.do(t, http.MethodGet, b.questionsPath(query), ""), http.StatusUnprocessableEntity, "validation_failed", field, "format")
	}
	rec := b.do(t, http.MethodGet, b.questionsPath("difficulty=x&unclassified=maybe"), "")
	if env := decodeError(t, rec, http.StatusUnprocessableEntity, "validation_failed"); len(env.Error.Details) != 2 {
		t.Errorf("details = %+v, want both problems", env.Error.Details)
	}
}

func TestListQuestionsOfAnUnknownOrMalformedExamIsNotFound(t *testing.T) {
	b := newBankAPI(t)

	for _, id := range []string{unknownID, "nope"} {
		wantError(t, b.do(t, http.MethodGet, examPath(id)+"/questions", ""), http.StatusNotFound, "not_found", "", "")
	}
}

// ── one question ───────────────────────────────────────────────────────────

func TestGetQuestionReturnsOptionsInStoredOrderWithLabelsAndCorrectFlags(t *testing.T) { // S04-2
	b := newBankAPI(t)

	rec := b.do(t, http.MethodGet, "/api/v1/questions/"+b.questionID(t, "vpc-1"), "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	q := decodeData(t, rec)
	opts := q["options"].([]any)
	if q["key"] != "vpc-1" || q["examId"] != b.examID || len(opts) != 3 {
		t.Fatalf("question = %v", q)
	}
	wantText, wantCorrect := []string{"wrong one", "right one", "wrong two"}, []bool{false, true, false}
	for i, o := range opts {
		m := o.(map[string]any)
		if m["text"] != wantText[i] || m["correct"] != wantCorrect[i] || m["label"] != string(rune('A'+i)) || m["position"] != float64(i) {
			t.Errorf("option %d = %v", i, m)
		}
	}
	if opts[1].(map[string]any)["rationale"] != "why" {
		t.Errorf("rationale missing: %v", opts[1])
	}
}

func TestGetOrDeleteAnUnknownOrMalformedQuestionIsNotFound(t *testing.T) {
	b := newBankAPI(t)

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		for _, id := range []string{unknownID, "nope"} {
			wantError(t, b.do(t, method, "/api/v1/questions/"+id, ""), http.StatusNotFound, "not_found", "", "")
		}
	}
}

func TestDeleteQuestionIsGoneFromReadAndListAndUpdatesTheInferredWeights(t *testing.T) { // S04-2
	b := newBankAPI(t)
	id := b.questionID(t, "vpc-1")

	rec := b.do(t, http.MethodDelete, "/api/v1/questions/"+id, "")

	if rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("status %d body %q", rec.Code, rec.Body)
	}
	wantError(t, b.do(t, http.MethodGet, "/api/v1/questions/"+id, ""), http.StatusNotFound, "not_found", "", "")
	wantError(t, b.do(t, http.MethodDelete, "/api/v1/questions/"+id, ""), http.StatusNotFound, "not_found", "", "")
	if got := b.keys(t, ""); slices.Contains(got, "vpc-1") || len(got) != 5 {
		t.Errorf("list = %v", got)
	}
	weights := map[string]float64{}
	for _, k := range b.classification(t, "Dominio")["categories"].([]any) {
		m := k.(map[string]any)
		weights[m["name"].(string)] = m["weightPercent"].(float64)
	}
	if weights["VPC"] != 50 || weights["Seguridad"] != 50 || b.exam(t)["questionTotal"] != float64(5) {
		t.Errorf("weights = %v, total %v, want 50/50 and 5 questions", weights, b.exam(t)["questionTotal"])
	}
}

// ── PATCH with classifications and categories ──────────────────────────────

// patchBank has four Dominio categories so manual weights can be 30/26/24/20.
func newPatchAPI(t *testing.T) bankAPI {
	t.Helper()
	return newBankAPI(t,
		apiQuestion("e1", "", 0, "", classified("Dominio", "Seguridad"), classified("Servicio", "Amazon S3")),
		apiQuestion("e2", "", 0, "", classified("Dominio", "Seguridad"), classified("Servicio", "Storage", "Amazon S3")),
		apiQuestion("e3", "", 0, "", classified("Dominio", "Costos"), classified("Servicio", "Storage")),
		apiQuestion("e4", "", 0, "", classified("Dominio", "Rendimiento"), classified("Servicio", "VPC")),
		apiQuestion("e5", "", 0, "", classified("Dominio", "Resiliencia"), classified("Servicio", "VPC", "Amazon S3")),
	)
}

func (b bankAPI) weightsBody(t *testing.T, values ...int) string {
	t.Helper()
	names := []string{"Seguridad", "Costos", "Rendimiento", "Resiliencia"}
	var items []string
	for i, name := range names[:len(values)] {
		items = append(items, fmt.Sprintf(`{"categoryId":%q,"percent":%d}`, b.categoryID(t, "Dominio", name), values[i]))
	}
	return `[` + strings.Join(items, ",") + `]`
}

func (b bankAPI) categoryMap(t *testing.T, classification, name string) map[string]any {
	t.Helper()
	for _, k := range b.classification(t, classification)["categories"].([]any) {
		if m := k.(map[string]any); m["name"] == name {
			return m
		}
	}
	t.Fatalf("no category %s/%s", classification, name)
	return nil
}

func TestPatchSavesManualWeightsAndTheExamFieldsUnderOneVersion(t *testing.T) { // S04 item 4
	b := newPatchAPI(t)

	rec := b.patch(t, fmt.Sprintf(`{"version":1,"name":"SAA-C03 v2","weights":%s}`, b.weightsBody(t, 30, 26, 24, 20)))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	exam := decodeData(t, rec)
	dom := exam["classifications"].([]any)[0].(map[string]any)
	var weights []float64
	for _, k := range dom["categories"].([]any) {
		weights = append(weights, k.(map[string]any)["weightPercent"].(float64))
	}
	if exam["name"] != "SAA-C03 v2" || exam["version"] != float64(2) || dom["weightMode"] != "manual" || !slices.Equal(weights, []float64{30, 26, 24, 20}) {
		t.Errorf("exam = %v", exam)
	}
}

func TestPatchRejectsWeightsThatDoNotSumTo100WritingNothing(t *testing.T) { // S04-3
	b := newPatchAPI(t)

	rec := b.patch(t, fmt.Sprintf(`{"version":1,"name":"Renamed","weights":%s}`, b.weightsBody(t, 30, 26, 24, 19)))

	wantError(t, rec, http.StatusUnprocessableEntity, "validation_failed", "weights", "weights_sum")
	if exam := b.exam(t); exam["name"] != "SAA-C03" || exam["version"] != float64(1) {
		t.Errorf("a rejected edit changed the exam: %v", exam)
	}
}

func TestPatchRejectsWeightsThatSkipACategoryNamingItsId(t *testing.T) { // S04-3
	b := newPatchAPI(t)
	missing := b.categoryID(t, "Dominio", "Resiliencia")

	rec := b.patch(t, fmt.Sprintf(`{"version":1,"weights":%s}`, b.weightsBody(t, 40, 30, 30)))

	env := decodeError(t, rec, http.StatusUnprocessableEntity, "validation_failed")
	for _, d := range env.Error.Details {
		if d.Field == "weights" && d.Rule == "weights_sum" && strings.Contains(d.Message, missing) {
			return
		}
	}
	t.Errorf("details = %+v, want weights_sum naming %s", env.Error.Details, missing)
}

func TestPatchInferredModeReturnsTheWeightsToTheQuestionShares(t *testing.T) { // S04-4
	b := newPatchAPI(t)
	b.patch(t, fmt.Sprintf(`{"version":1,"weights":%s}`, b.weightsBody(t, 30, 26, 24, 20)))

	rec := b.patch(t, `{"version":2,"weightMode":"inferred"}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	dom := decodeData(t, rec)["classifications"].([]any)[0].(map[string]any)
	var weights []float64
	for _, k := range dom["categories"].([]any) {
		weights = append(weights, k.(map[string]any)["weightPercent"].(float64))
	}
	if dom["weightMode"] != "inferred" || !slices.Equal(weights, []float64{40, 20, 20, 20}) {
		t.Errorf("mode %v weights %v, want inferred 40/20/20/20", dom["weightMode"], weights)
	}
}

func TestPatchPaceIsAPairAndNullClearsIt(t *testing.T) { // S04-5
	b := newPatchAPI(t)
	seg := b.categoryID(t, "Dominio", "Seguridad")
	body := func(pace string) string {
		return fmt.Sprintf(`{"version":%d,"categories":[{"id":%q%s}]}`, b.version(t), seg, pace)
	}

	for _, bad := range []string{`,"paceQuestions":10`, `,"paceMinutes":20`, `,"paceQuestions":10,"paceMinutes":null`, `,"paceQuestions":0,"paceMinutes":5`} {
		wantError(t, b.patch(t, body(bad)), http.StatusUnprocessableEntity, "validation_failed", "categories[0].pace", "pace_pair")
	}
	rec := b.patch(t, body(`,"paceQuestions":10,"paceMinutes":20`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if k := b.categoryMap(t, "Dominio", "Seguridad"); k["paceQuestions"] != float64(10) || k["paceMinutes"] != float64(20) {
		t.Errorf("category = %v", k)
	}
	if rec := b.patch(t, body(`,"paceQuestions":null,"paceMinutes":null`)); rec.Code != http.StatusOK {
		t.Fatalf("clear: %d %s", rec.Code, rec.Body)
	}
	if k := b.categoryMap(t, "Dominio", "Seguridad"); k["paceQuestions"] != nil || k["paceMinutes"] != nil {
		t.Errorf("category = %v, want the pace cleared", k)
	}
}

func TestPatchRenamesClassificationsAndCategories(t *testing.T) {
	b := newPatchAPI(t)
	seg, svc := b.categoryID(t, "Dominio", "Seguridad"), b.classification(t, "Servicio")["id"]

	rec := b.patch(t, fmt.Sprintf(`{"version":1,"classifications":[{"id":%q,"name":"Servicios"}],"categories":[{"id":%q,"name":"Seguridad y cumplimiento"}]}`, svc, seg))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	if k := b.categoryMap(t, "Dominio", "Seguridad y cumplimiento"); k["key"] != "seguridad-y-cumplimiento" || k["id"] != seg {
		t.Errorf("category = %v", k)
	}
	b.classification(t, "Servicios")
	row := decodeList(t, b.do(t, http.MethodGet, b.questionsPath("q=e1"), ""))[0]
	cats := row["categories"].([]any)
	if cats[0].(map[string]any)["name"] != "Seguridad y cumplimiento" || cats[1].(map[string]any)["classification"] != "Servicios" {
		t.Errorf("question categories = %v", cats)
	}
}

func TestPatchRenameToAnExistingNameDifferingOnlyInAccentsIsADuplicate(t *testing.T) { // S04-9
	b := newBankAPI(t,
		apiQuestion("a", "", 0, "", classified("Dominio", "Señales")),
		apiQuestion("b", "", 0, "", classified("Dominio", "Otra")),
	)
	other := b.categoryID(t, "Dominio", "Otra")

	rec := b.patch(t, fmt.Sprintf(`{"version":1,"categories":[{"id":%q,"name":"Senales"}]}`, other))

	wantError(t, rec, http.StatusConflict, "conflict", "categories[0].name", "duplicate_name")
	if b.version(t) != 1 {
		t.Error("version moved")
	}
}

func TestPatchSwapOfThePrimaryIsRefusedWhenAQuestionHasTwoCategories(t *testing.T) { // S04-8
	b := newPatchAPI(t)

	rec := b.patch(t, fmt.Sprintf(`{"version":1,"primaryClassificationId":%q}`, b.classification(t, "Servicio")["id"]))

	env := decodeError(t, rec, http.StatusUnprocessableEntity, "validation_failed")
	for _, d := range env.Error.Details {
		if d.Field == "primaryClassificationId" && d.Rule == "primary_single_valued" && strings.Contains(d.Message, "e2") && strings.Contains(d.Message, "e5") {
			return
		}
	}
	t.Errorf("details = %+v, want primary_single_valued naming e2 and e5", env.Error.Details)
}

func TestPatchSwapOfThePrimarySucceedsWhenTheNewOneIsSingleValued(t *testing.T) {
	b := newBankAPI(t,
		apiQuestion("a", "", 0, "", classified("Dominio", "X"), classified("Servicio", "P")),
		apiQuestion("b", "", 0, "", classified("Dominio", "X"), classified("Servicio", "Q")),
		apiQuestion("c", "", 0, "", classified("Dominio", "Y"), classified("Servicio", "P")),
	)

	rec := b.patch(t, fmt.Sprintf(`{"version":1,"primaryClassificationId":%q}`, b.classification(t, "Servicio")["id"]))

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	svc, dom := b.classification(t, "Servicio"), b.classification(t, "Dominio")
	if svc["position"] != float64(0) || dom["position"] != float64(1) || svc["weightMode"] != "inferred" {
		t.Fatalf("servicio %v, dominio %v", svc, dom)
	}
	p := b.categoryMap(t, "Servicio", "P")
	if p["weightPercent"] != float64(67) || b.categoryMap(t, "Dominio", "X")["weightPercent"] != nil {
		t.Errorf("P = %v: weights exist on the primary only", p)
	}
}

func TestPatchWithAStaleVersionIsAConflict(t *testing.T) {
	b := newPatchAPI(t)

	wantError(t, b.patch(t, `{"version":9,"weightMode":"inferred"}`), http.StatusConflict, "conflict", "version", "stale_version")
}

func TestPatchShapeProblemsAreValidationErrors(t *testing.T) {
	b := newPatchAPI(t)
	seg := b.categoryID(t, "Dominio", "Seguridad")
	cases := []struct {
		name, body, field, rule string
	}{
		{"nothing to change", `{"version":1}`, "body", "required"},
		{"empty lists change nothing", `{"version":1,"categories":[],"classifications":[]}`, "body", "required"},
		{"classifications null", `{"version":1,"classifications":null}`, "classifications", "required"},
		{"categories null", `{"version":1,"categories":null}`, "categories", "required"},
		{"weights null", `{"version":1,"weights":null}`, "weights", "required"},
		{"primaryClassificationId null", `{"version":1,"primaryClassificationId":null}`, "primaryClassificationId", "required"},
		{"primaryClassificationId empty", `{"version":1,"primaryClassificationId":""}`, "primaryClassificationId", "required"},
		{"weightMode null", `{"version":1,"weightMode":null}`, "weightMode", "required"},
		{"weightMode manual is not accepted", `{"version":1,"weightMode":"manual"}`, "weightMode", "format"},
		{"a category without id", `{"version":1,"categories":[{"name":"X"}]}`, "categories[0].id", "required"},
		{"a category name null", fmt.Sprintf(`{"version":1,"categories":[{"id":%q,"name":null}]}`, seg), "categories[0].name", "required"},
		{"a classification without id", `{"version":1,"classifications":[{"name":"X"}]}`, "classifications[0].id", "required"},
		{"a weight without percent", fmt.Sprintf(`{"version":1,"weights":[{"categoryId":%q}]}`, seg), "weights[0].percent", "required"},
		{"a weight without category", `{"version":1,"weights":[{"percent":10}]}`, "weights[0].categoryId", "required"},
		{"an unknown category", `{"version":1,"categories":[{"id":"0abcdef0-0000-7000-8000-0000000dead0","name":"X"}]}`, "categories[0].id", "not_found"},
		{"an unknown primary", `{"version":1,"primaryClassificationId":"0abcdef0-0000-7000-8000-0000000dead0"}`, "primaryClassificationId", "not_found"},
		{"an over-long category name", fmt.Sprintf(`{"version":1,"categories":[{"id":%q,"name":%q}]}`, seg, strings.Repeat("x", 81)), "categories[0].name", "max_length"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantError(t, b.patch(t, tc.body), http.StatusUnprocessableEntity, "validation_failed", tc.field, tc.rule)
		})
	}
}

func TestPatchOfAnUnknownExamIsNotFound(t *testing.T) {
	b := newPatchAPI(t)

	wantError(t, b.do(t, http.MethodPatch, examPath(unknownID), `{"version":1,"weightMode":"inferred"}`), http.StatusNotFound, "not_found", "", "")
}

// ── merge ──────────────────────────────────────────────────────────────────

func (b bankAPI) mergePath() string { return examPath(b.examID) + "/categories/merge" }

func (b bankAPI) merge(t *testing.T, from, into string) *httptest.ResponseRecorder {
	t.Helper()
	return b.do(t, http.MethodPost, b.mergePath(), fmt.Sprintf(`{"from":%q,"into":%q}`, from, into))
}

func TestMergeReturnsTheExamWithTheLinksMovedAndTheVersionIncremented(t *testing.T) { // S04-6
	b := newPatchAPI(t)
	storage, s3 := b.categoryID(t, "Servicio", "Storage"), b.categoryID(t, "Servicio", "Amazon S3")

	rec := b.merge(t, storage, s3)

	if rec.Code != http.StatusOK {
		t.Fatalf("status %d %s", rec.Code, rec.Body)
	}
	exam := decodeData(t, rec)
	if exam["version"] != float64(2) {
		t.Errorf("version = %v", exam["version"])
	}
	for _, k := range exam["classifications"].([]any)[1].(map[string]any)["categories"].([]any) {
		if k.(map[string]any)["name"] == "Storage" {
			t.Error("Storage is still listed")
		}
	}
	if n := b.categoryMap(t, "Servicio", "Amazon S3")["questionCount"]; n != float64(4) {
		t.Errorf("Amazon S3 questionCount = %v, want 4", n)
	}
	row := decodeList(t, b.do(t, http.MethodGet, b.questionsPath("q=e2"), ""))[0]
	count := 0
	for _, c := range row["categories"].([]any) {
		if c.(map[string]any)["name"] == "Amazon S3" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("e2 had both ends and must keep one link: %v", row["categories"])
	}
}

func TestMergeScopeErrorsAreUnprocessable(t *testing.T) { // S04-7
	b := newPatchAPI(t)
	seg, s3, storage := b.categoryID(t, "Dominio", "Seguridad"), b.categoryID(t, "Servicio", "Amazon S3"), b.categoryID(t, "Servicio", "Storage")
	if rec := b.merge(t, storage, s3); rec.Code != http.StatusOK {
		t.Fatalf("first merge: %d", rec.Code)
	}

	wantError(t, b.merge(t, seg, s3), http.StatusUnprocessableEntity, "validation_failed", "into", "merge_scope")
	wantError(t, b.merge(t, storage, s3), http.StatusUnprocessableEntity, "validation_failed", "from", "merge_scope")
	wantError(t, b.merge(t, s3, s3), http.StatusUnprocessableEntity, "validation_failed", "into", "merge_scope")
}

func TestMergeBodyProblems(t *testing.T) {
	b := newPatchAPI(t)
	s3 := b.categoryID(t, "Servicio", "Amazon S3")
	for _, tc := range []struct{ name, body, field, rule string }{
		{"from missing", fmt.Sprintf(`{"into":%q}`, s3), "from", "required"},
		{"into missing", fmt.Sprintf(`{"from":%q}`, s3), "into", "required"},
		{"from null", fmt.Sprintf(`{"from":null,"into":%q}`, s3), "from", "required"},
		{"from not a uuid", fmt.Sprintf(`{"from":"storage","into":%q}`, s3), "from", "format"},
		{"into not a uuid", fmt.Sprintf(`{"from":%q,"into":"s3"}`, s3), "into", "format"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wantError(t, b.do(t, http.MethodPost, b.mergePath(), tc.body), http.StatusUnprocessableEntity, "validation_failed", tc.field, tc.rule)
		})
	}
	wantError(t, b.do(t, http.MethodPost, b.mergePath(), `not json`), http.StatusUnprocessableEntity, "validation_failed", "body", "json")
}

func TestMergeInAnUnknownExamIsNotFound(t *testing.T) {
	b := newPatchAPI(t)
	s3 := b.categoryID(t, "Servicio", "Amazon S3")

	rec := b.do(t, http.MethodPost, examPath(unknownID)+"/categories/merge", fmt.Sprintf(`{"from":%q,"into":%q}`, s3, s3))

	wantError(t, rec, http.StatusNotFound, "not_found", "", "")
}
