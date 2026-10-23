package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/app/usecase"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

const (
	clsDominio   = "0abcdef0-0000-7000-8000-0000000c1001"
	clsServicio  = "0abcdef0-0000-7000-8000-0000000c1002"
	catSeguridad = "0abcdef0-0000-7000-8000-0000000ca001"
	catCostos    = "0abcdef0-0000-7000-8000-0000000ca002"
	catVPC       = "0abcdef0-0000-7000-8000-0000000ca003"
	catLambda    = "0abcdef0-0000-7000-8000-0000000ca004"
)

var attemptsT0 = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

type attemptsAPI struct {
	apiEnv
	examID   string
	bank     *testsupport.FakeQuestionBank
	attempts *testsupport.FakeAttemptRepo
}

func dominioRef(name string) domain.CategoryRef {
	return domain.CategoryRef{Classification: "Dominio", Category: name}
}

func servicioRef(name string) domain.CategoryRef {
	return domain.CategoryRef{Classification: "Servicio", Category: name}
}

// apiAttemptQuestion: four options "<key>/a".."<key>/d", "<key>/b" correct,
// every option with a rationale and the question with an explanation, so a
// leak of the answer key cannot go unseen.
func apiAttemptQuestion(examID, key string, refs ...domain.CategoryRef) domain.Question {
	q := domain.Question{ID: "q-" + key, ExamID: examID, Key: key, Type: domain.QuestionSingle, Stem: "stem " + key,
		Explanation: "explanation of " + key, Version: 1, Categories: refs}
	for i, letter := range []string{"a", "b", "c", "d"} {
		q.Options = append(q.Options, domain.Option{Text: key + "/" + letter, Correct: i == 1, Rationale: "why " + key + "/" + letter})
	}
	return q
}

func apiAttemptQuestions(examID, prefix string, n int, refs ...domain.CategoryRef) []domain.Question {
	out := make([]domain.Question, n)
	for i := range out {
		out[i] = apiAttemptQuestion(examID, fmt.Sprintf("%s-%02d", prefix, i), refs...)
	}
	return out
}

// attemptsBank is the exam (65 questions / 130 min / 72%) of the slice 06a
// tests: Dominio is primary with 50/50 weights and 40 questions each; Servicio
// paces VPC at 10 questions in 20 min (10 questions) and Lambda at 4 in 10
// (4 questions); "Timed Set 3" holds three unclassified questions numbered 2,
// 1 and 3, the third a multi question with 2 correct options.
func attemptsBank(examID string) domain.Bank {
	ten, twenty, four := 10, 20, 4
	bank := domain.Bank{Classifications: []domain.Classification{
		{ID: clsDominio, ExamID: examID, Name: "Dominio", Fold: "dominio", Position: domain.PositionPrimary, WeightMode: domain.WeightManual, Categories: []domain.Category{
			{ID: catSeguridad, ClassificationID: clsDominio, Name: "Seguridad", Fold: "seguridad", WeightPercent: 50},
			{ID: catCostos, ClassificationID: clsDominio, Name: "Costos", Fold: "costos", WeightPercent: 50},
		}},
		{ID: clsServicio, ExamID: examID, Name: "Servicio", Fold: "servicio", Position: domain.PositionSecondary, WeightMode: domain.WeightInferred, Categories: []domain.Category{
			{ID: catVPC, ClassificationID: clsServicio, Name: "VPC", Fold: "vpc", PaceQuestions: &ten, PaceMinutes: &twenty},
			{ID: catLambda, ClassificationID: clsServicio, Name: "Lambda", Fold: "lambda", PaceQuestions: &four, PaceMinutes: &ten},
		}},
	}}
	bank.Questions = append(bank.Questions, apiAttemptQuestions(examID, "seg", 40, dominioRef("Seguridad"))...)
	bank.Questions = append(bank.Questions, apiAttemptQuestions(examID, "cos", 40, dominioRef("Costos"))...)
	bank.Questions = append(bank.Questions, apiAttemptQuestions(examID, "vpc", 10, servicioRef("VPC"))...)
	bank.Questions = append(bank.Questions, apiAttemptQuestions(examID, "lam", 4, servicioRef("Lambda"))...)
	inSet := func(q domain.Question, number int) domain.Question {
		q.Origin = &domain.Origin{Set: "Timed Set 3", Number: number, NoteRef: "03 · Route 53 (diap. 12)"}
		return q
	}
	multi := apiAttemptQuestion(examID, "ts3-m")
	multi.Type = domain.QuestionMulti
	multi.Options[0].Correct, multi.Options[1].Correct = true, true // two correct: a and b
	bank.Questions = append(bank.Questions,
		inSet(apiAttemptQuestion(examID, "ts3-b"), 2), inSet(apiAttemptQuestion(examID, "ts3-a"), 1), inSet(multi, 3))
	return bank
}

func newAttemptsAPI(t *testing.T) attemptsAPI {
	t.Helper()
	subjects := testsupport.NewFakeSubjectDirectory(subjectA, subjectB)
	repo := testsupport.NewFakeExamRepo(subjects)
	bank := testsupport.NewFakeQuestionBank(repo)
	attempts := testsupport.NewFakeAttemptRepo(repo)
	clock := &testsupport.FixedClock{T: attemptsT0}
	ids := &testsupport.SeqIDGen{N: 100}
	deps := Deps{
		CreateExam:     usecase.NewCreateExamService(repo, subjects, clock, ids),
		PreviewSession: usecase.NewPreviewSessionService(repo, bank),
		StartAttempt:   usecase.NewStartAttemptService(repo, bank, attempts, clock, ids, testsupport.FixedSeeds{Seed: 99}),
	}
	env := apiEnv{router: httpx.NewServer(httpx.Config{MaxBodyBytes: 1 << 20}, Register(deps))}
	exam := env.createExam(t, subjectA, "SAA-C03")
	api := attemptsAPI{apiEnv: env, examID: exam["id"].(string), bank: bank, attempts: attempts}
	bank.Set(api.examID, attemptsBank(api.examID))
	return api
}

func (a attemptsAPI) startPath() string   { return examPath(a.examID) + "/attempts" }
func (a attemptsAPI) previewPath() string { return examPath(a.examID) + "/sessions/preview" }

const (
	simulationConfig = `{"selection":{"kind":"simulation"}}`
	randomConfig     = `{"selection":{"kind":"random","count":5},"timing":{"kind":"untimed"},"feedback":"end"}`
	originSetConfig  = `{"selection":{"kind":"origin-set","set":"Timed Set 3"},"timing":{"kind":"untimed"},"feedback":"each"}`
)

func servicioConfig(count string, timing string) string {
	return fmt.Sprintf(`{"selection":{"kind":"categories","classificationId":%q,"categoryIds":[%q,%q],"count":%s},"timing":%s,"feedback":"end"}`,
		clsServicio, catVPC, catLambda, count, timing)
}

func (a attemptsAPI) start(t *testing.T, config string, seed string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"config":` + config
	if seed != "" {
		body += `,"seed":` + seed
	}
	return a.do(t, http.MethodPost, a.startPath(), body+`}`)
}

func (a attemptsAPI) mustStart(t *testing.T, config string, seed string) map[string]any {
	t.Helper()
	rec := a.start(t, config, seed)
	if rec.Code != http.StatusCreated {
		t.Fatalf("start: status %d, body %s", rec.Code, rec.Body)
	}
	return decodeData(t, rec)
}

func (a attemptsAPI) preview(t *testing.T, config string) map[string]any {
	t.Helper()
	rec := a.do(t, http.MethodPost, a.previewPath(), `{"config":`+config+`}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("preview: status %d, body %s", rec.Code, rec.Body)
	}
	return decodeData(t, rec)
}

func errorOf(t *testing.T, rec *httptest.ResponseRecorder) (code string, details []map[string]any) {
	t.Helper()
	var envelope struct {
		Error struct {
			Code    string           `json:"code"`
			Details []map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body, err)
	}
	return envelope.Error.Code, envelope.Error.Details
}

func walkKeys(v any, visit func(key string)) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			visit(k)
			walkKeys(child, visit)
		}
	case []any:
		for _, child := range x {
			walkKeys(child, visit)
		}
	}
}

func itemsOf(t *testing.T, attempt map[string]any) []map[string]any {
	t.Helper()
	raw, ok := attempt["items"].([]any)
	if !ok {
		t.Fatalf("attempt has no items array: %v", attempt)
	}
	out := make([]map[string]any, len(raw))
	for i, it := range raw {
		out[i] = it.(map[string]any)
	}
	return out
}

// ── S06a-4, BR-A4 ───────────────────────────────────────────────────────────

func TestStartResponseHasNoCorrectnessKeyAtAnyDepth(t *testing.T) { // S06a-4
	for name, config := range map[string]string{"origin-set with feedback each": originSetConfig, "random": randomConfig, "simulation": simulationConfig} {
		t.Run(name, func(t *testing.T) {
			a := newAttemptsAPI(t)
			rec := a.start(t, config, "")
			if rec.Code != http.StatusCreated {
				t.Fatalf("status %d, body %s", rec.Code, rec.Body)
			}

			var doc any
			if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
				t.Fatal(err)
			}
			keys := 0
			walkKeys(doc, func(key string) {
				keys++
				lower := strings.ToLower(key)
				for _, banned := range []string{"correct", "rationale", "explanation"} {
					if strings.Contains(lower, banned) {
						t.Errorf("the start response has the key %q", key)
					}
				}
			})
			if keys < 20 {
				t.Errorf("the walk saw only %d keys: the response is not what the test thinks", keys)
			}
			body := rec.Body.String()
			if strings.Contains(body, "why ") || strings.Contains(body, "explanation of") {
				t.Errorf("rationale or explanation text leaked into the body: %.300s", body)
			}
		})
	}
}

func TestMultiItemCarriesChooseCountAndSingleDoesNot(t *testing.T) { // S06a-7
	a := newAttemptsAPI(t)

	items := itemsOf(t, a.mustStart(t, originSetConfig, ""))

	if len(items) != 3 {
		t.Fatalf("items = %d, want the 3 questions of Timed Set 3", len(items))
	}
	for _, it := range items {
		switch it["stem"] {
		case "stem ts3-m":
			if it["type"] != "multi" || it["chooseCount"] != float64(2) {
				t.Errorf("multi item = %v, want type multi and chooseCount 2", it)
			}
		default:
			if _, has := it["chooseCount"]; has || it["type"] != "single" {
				t.Errorf("single item = %v, want type single and no chooseCount", it)
			}
		}
	}
}

// ── shape ───────────────────────────────────────────────────────────────────

func TestStartAnUntimedAttemptAnswers201WithTheStartShape(t *testing.T) {
	a := newAttemptsAPI(t)

	rec := a.start(t, randomConfig, "")

	if rec.Code != http.StatusCreated {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	data := decodeData(t, rec)
	id, _ := data["id"].(string)
	if rec.Header().Get("Location") != "/api/v1/attempts/"+id || id == "" {
		t.Errorf("Location = %q, want /api/v1/attempts/%s", rec.Header().Get("Location"), id)
	}
	if data["examId"] != a.examID || data["status"] != "in_progress" || data["startedAt"] != "2026-10-23T09:00:00Z" {
		t.Errorf("header = %v", data)
	}
	for _, absent := range []string{"deadlineAt", "remainingSeconds"} {
		if _, has := data[absent]; has {
			t.Errorf("an untimed attempt has %s", absent)
		}
	}
	if data["suggestedMinutes"] != float64(10) { // 5 questions × the exam pace of 2 min (Dominio is the pace classification)
		t.Errorf("suggestedMinutes = %v, want 10", data["suggestedMinutes"])
	}
	wantConfig := map[string]any{"selection": map[string]any{"kind": "random", "count": float64(5)}, "timing": map[string]any{"kind": "untimed"}, "feedback": "end"}
	if !reflect.DeepEqual(data["config"], wantConfig) {
		t.Errorf("config = %v, want the request config echoed: %v", data["config"], wantConfig)
	}
	items := itemsOf(t, data)
	if len(items) != 5 {
		t.Fatalf("items = %d, want 5", len(items))
	}
	for i, it := range items {
		opts := it["options"].([]any)
		if it["position"] != float64(i) || len(opts) != 4 || it["stem"] == "" {
			t.Fatalf("item %d = %v", i, it)
		}
		for j, o := range opts {
			opt := o.(map[string]any)
			if opt["label"] != string(rune('A'+j)) || opt["text"] == "" {
				t.Errorf("item %d option %d = %v, want label %c and its text", i, j, opt, 'A'+j)
			}
			if _, has := opt["image"]; has {
				t.Errorf("a text option has an image key: %v", opt)
			}
		}
	}
}

func TestStartATimedAttemptCarriesDeadlineAndRemainingSeconds(t *testing.T) { // FR-9 deadline from the server clock
	a := newAttemptsAPI(t)

	data := a.mustStart(t, servicioConfig(`"all"`, `{"kind":"timed","minutes":30}`), "")

	if data["deadlineAt"] != "2026-10-23T09:30:00Z" || data["remainingSeconds"] != float64(1800) {
		t.Errorf("deadlineAt %v remainingSeconds %v, want 09:30:00Z and 1800", data["deadlineAt"], data["remainingSeconds"])
	}
	wantConfig := map[string]any{
		"selection": map[string]any{"kind": "categories", "classificationId": clsServicio, "categoryIds": []any{catVPC, catLambda}, "count": "all"},
		"timing":    map[string]any{"kind": "timed", "minutes": float64(30)}, "feedback": "end",
	}
	if !reflect.DeepEqual(data["config"], wantConfig) {
		t.Errorf("config = %v, want %v", data["config"], wantConfig)
	}
}

func TestStartASimulationEchoesAConfigWithoutTimingAndTimesItWithTheExam(t *testing.T) { // AC-14, FR-7
	a := newAttemptsAPI(t)

	data := a.mustStart(t, simulationConfig, "")

	if !reflect.DeepEqual(data["config"], map[string]any{"selection": map[string]any{"kind": "simulation"}}) {
		t.Errorf("config = %v, want only the selection", data["config"])
	}
	if data["deadlineAt"] != "2026-10-23T11:10:00Z" || data["remainingSeconds"] != float64(7800) || data["suggestedMinutes"] != float64(130) {
		t.Errorf("deadlineAt %v remaining %v suggested %v, want the exam's 130 min", data["deadlineAt"], data["remainingSeconds"], data["suggestedMinutes"])
	}
	if got := len(itemsOf(t, data)); got != 65 {
		t.Errorf("items = %d, want 65", got)
	}
}

// ── S06a-5, S06a-6, AC-15 ───────────────────────────────────────────────────

func TestSimulationSentWithTimingIs422LockedBySimulation(t *testing.T) { // S06a-5
	a := newAttemptsAPI(t)

	rec := a.start(t, `{"selection":{"kind":"simulation"},"timing":{"kind":"timed","minutes":30}}`, "")

	code, details := errorOf(t, rec)
	if rec.Code != http.StatusUnprocessableEntity || code != "validation_failed" || len(details) != 1 ||
		details[0]["rule"] != "locked_by_simulation" || details[0]["field"] != "config.timing" {
		t.Errorf("status %d, code %s, details %v; want 422 locked_by_simulation on config.timing", rec.Code, code, details)
	}
	if a.attempts.Count() != 0 {
		t.Error("a rejected start stored an attempt")
	}
}

func TestSimulationSentWithFeedbackIs422LockedBySimulation(t *testing.T) {
	a := newAttemptsAPI(t)

	rec := a.do(t, http.MethodPost, a.previewPath(), `{"config":{"selection":{"kind":"simulation"},"feedback":"each"}}`)

	_, details := errorOf(t, rec)
	if rec.Code != http.StatusUnprocessableEntity || len(details) != 1 || details[0]["rule"] != "locked_by_simulation" || details[0]["field"] != "config.feedback" {
		t.Errorf("status %d, details %v; want 422 locked_by_simulation on config.feedback", rec.Code, details)
	}
}

func TestASecondStartWhileOneIsInProgressIs409AndNamesTheOpenAttempt(t *testing.T) { // S06a-6, BR-A1
	a := newAttemptsAPI(t)
	first := a.mustStart(t, randomConfig, "")

	rec := a.start(t, randomConfig, "")

	code, details := errorOf(t, rec)
	if rec.Code != http.StatusConflict || code != "conflict" || len(details) != 1 {
		t.Fatalf("status %d, code %s, details %v; want 409 conflict", rec.Code, code, details)
	}
	d := details[0]
	if d["rule"] != "attempt_in_progress" || d["field"] != "attemptId" || !strings.Contains(d["message"].(string), first["id"].(string)) {
		t.Errorf("detail = %v, want attempt_in_progress, field attemptId and the id %v in the message", d, first["id"])
	}
}

func TestStartWithTooFewQuestionsInACategoryIs422WithCategoryNeededAvailable(t *testing.T) { // AC-15
	a := newAttemptsAPI(t)
	bank := a.bank.Bank(a.examID)
	kept := bank.Questions[:0]
	seg := 0
	for _, q := range bank.Questions {
		if q.Categories != nil && q.Categories[0].Category == "Seguridad" {
			if seg++; seg > 12 {
				continue
			}
		}
		kept = append(kept, q)
	}
	bank.Questions = kept
	a.bank.Set(a.examID, bank)

	rec := a.start(t, simulationConfig, "")

	code, details := errorOf(t, rec)
	if rec.Code != http.StatusUnprocessableEntity || code != "validation_failed" || len(details) != 1 {
		t.Fatalf("status %d, code %s, details %v", rec.Code, code, details)
	}
	d := details[0]
	if d["rule"] != "insufficient_questions" || d["field"] != "config" || d["category"] != "Seguridad" || d["needed"] != float64(33) || d["available"] != float64(12) {
		t.Errorf("detail = %v, want insufficient_questions {category Seguridad, needed 33, available 12}", d)
	}
}

func TestAnEmptyOriginSetIs422InsufficientWithNeeded1Available0(t *testing.T) { // slice 06a §1.3
	a := newAttemptsAPI(t)

	rec := a.start(t, `{"selection":{"kind":"origin-set","set":"Timed Set 9"},"timing":{"kind":"untimed"},"feedback":"end"}`, "")

	_, details := errorOf(t, rec)
	if rec.Code != http.StatusUnprocessableEntity || len(details) != 1 || details[0]["rule"] != "insufficient_questions" ||
		details[0]["category"] != "Timed Set 9" || details[0]["needed"] != float64(1) || details[0]["available"] != float64(0) {
		t.Errorf("status %d, details %v", rec.Code, details)
	}
}

// ── preview: S06a-1, S06a-2 ─────────────────────────────────────────────────

func TestPreviewOfASimulationOnTheFixture(t *testing.T) { // S06a-1
	a := newAttemptsAPI(t)

	p := a.preview(t, simulationConfig)

	if p["poolSize"] != float64(80) || p["drawCount"] != float64(65) || p["suggestedMinutes"] != float64(130) {
		t.Errorf("preview = %v, want pool 80 (≥ 65), draw 65, 130 min", p)
	}
	if shortages, ok := p["shortages"].([]any); !ok || len(shortages) != 0 {
		t.Errorf("shortages = %v, want an empty array", p["shortages"])
	}
}

func TestPreviewOfAllQuestionsOfVPCAndLambdaIs30Minutes(t *testing.T) { // S06a-2, AC-16 first case
	a := newAttemptsAPI(t)

	p := a.preview(t, servicioConfig(`"all"`, `{"kind":"untimed"}`))

	if p["poolSize"] != float64(14) || p["drawCount"] != float64(14) || p["suggestedMinutes"] != float64(30) {
		t.Errorf("preview = %v, want pool 14, draw 14, 30 min (10×2 + 4×2.5)", p)
	}
}

func TestPreviewListsShortagesWithoutFailing(t *testing.T) { // slice 06a §1.2
	a := newAttemptsAPI(t)
	bank := a.bank.Bank(a.examID)
	bank.Questions = bank.Questions[40:] // no Seguridad at all
	a.bank.Set(a.examID, bank)

	p := a.preview(t, simulationConfig)

	want := []any{map[string]any{"category": "Seguridad", "needed": float64(33), "available": float64(0)}}
	if !reflect.DeepEqual(p["shortages"], want) {
		t.Errorf("shortages = %v, want %v", p["shortages"], want)
	}
}

func TestPreviewIsAPureReadAndWorksWithAnAttemptOpen(t *testing.T) { // slice 06a §1.2
	a := newAttemptsAPI(t)

	a.preview(t, randomConfig)
	if a.attempts.Count() != 0 {
		t.Fatal("a preview stored an attempt")
	}
	a.mustStart(t, randomConfig, "") // not blocked by the earlier previews
	if rec := a.do(t, http.MethodPost, a.previewPath(), `{"config":`+randomConfig+`}`); rec.Code != http.StatusOK {
		t.Errorf("preview with an attempt open: status %d, want 200", rec.Code)
	}
}

// ── S06a-3, AC-17, AC-21 ────────────────────────────────────────────────────

func TestStartWithSevenOfVPCAndLambdaDrawingFiveAndTwoSuggests15Minutes(t *testing.T) { // S06a-3, AC-16 second case
	a := newAttemptsAPI(t)
	exam, _ := domain.NewExam("e", subjectA, "x", 65, 130, 72, attemptsT0)
	exam.ID = a.examID
	bank := a.bank.Bank(a.examID)
	cfg := domain.SessionConfig{
		Selection: domain.Selection{Kind: domain.SelectCategories, ClassificationID: clsServicio, CategoryIDs: []string{catVPC, catLambda}, Count: 7},
		Timing:    &domain.Timing{Kind: domain.Untimed}, Feedback: domain.FeedbackEnd,
	}
	seed := uint64(0)
	for ; seed < 5000; seed++ {
		draw, err := domain.DrawSession(exam, bank, cfg, seed)
		if err != nil {
			t.Fatal(err)
		}
		lambdas := 0
		for _, d := range draw.Questions {
			if d.Question.Categories[0].Category == "Lambda" {
				lambdas++
			}
		}
		if lambdas == 2 {
			break
		}
	}

	data := a.mustStart(t, servicioConfig("7", `{"kind":"untimed"}`), fmt.Sprint(seed))

	if data["suggestedMinutes"] != float64(15) {
		t.Errorf("seed %d: suggestedMinutes = %v, want ⌈5×2 + 2×2.5⌉ = 15", seed, data["suggestedMinutes"])
	}
	if got := len(itemsOf(t, data)); got != 7 {
		t.Errorf("items = %d, want 7", got)
	}
}

func TestTheSameSeedGivesTheSameQuestionsAndOptionOrderOverHTTP(t *testing.T) { // AC-17
	const seed = "18446744073709551615" // the largest uint64 the wire accepts
	first := newAttemptsAPI(t).mustStart(t, randomConfig, seed)
	second := newAttemptsAPI(t).mustStart(t, randomConfig, seed)
	other := newAttemptsAPI(t).mustStart(t, randomConfig, "12345")

	if !reflect.DeepEqual(first["items"], second["items"]) {
		t.Error("same seed, different items")
	}
	if reflect.DeepEqual(first["items"], other["items"]) {
		t.Error("another seed drew the same items")
	}
}

func TestAnOriginSetSessionPresentsItsQuestionsInOriginNumberOrder(t *testing.T) { // AC-21
	a := newAttemptsAPI(t)

	items := itemsOf(t, a.mustStart(t, originSetConfig, "7"))

	var stems []string
	for _, it := range items {
		stems = append(stems, it["stem"].(string))
	}
	if !reflect.DeepEqual(stems, []string{"stem ts3-a", "stem ts3-b", "stem ts3-m"}) {
		t.Errorf("stems = %v, want origin.number order 1, 2, 3", stems)
	}
}

// ── request validation ──────────────────────────────────────────────────────

func TestStartRequestValidation(t *testing.T) {
	a := newAttemptsAPI(t)
	for name, tc := range map[string]struct {
		body        string
		field, rule string
	}{
		"no config":                         {`{}`, "config", "required"},
		"config null":                       {`{"config":null}`, "config", "required"},
		"no selection":                      {`{"config":{"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection", "required"},
		"no selection kind":                 {`{"config":{"selection":{"count":5},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.kind", "required"},
		"unknown selection kind":            {`{"config":{"selection":{"kind":"everything"},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.kind", "format"},
		"random without count":              {`{"config":{"selection":{"kind":"random"},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.count", "required"},
		"random count 0":                    {`{"config":{"selection":{"kind":"random","count":0},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.count", "range"},
		"random count 201":                  {`{"config":{"selection":{"kind":"random","count":201},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.count", "range"},
		"random count all":                  {`{"config":{"selection":{"kind":"random","count":"all"},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.count", "format"},
		"random count fractional":           {`{"config":{"selection":{"kind":"random","count":2.5},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.count", "format"},
		"categories count word":             {`{"config":{"selection":{"kind":"categories","classificationId":"` + clsServicio + `","categoryIds":["` + catVPC + `"],"count":"some"},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.count", "format"},
		"categories bad classification":     {`{"config":{"selection":{"kind":"categories","classificationId":"x","categoryIds":["` + catVPC + `"],"count":"all"},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.classificationId", "format"},
		"categories bad category id":        {`{"config":{"selection":{"kind":"categories","classificationId":"` + clsServicio + `","categoryIds":["nope"],"count":"all"},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.categoryIds[0]", "format"},
		"categories no ids":                 {`{"config":{"selection":{"kind":"categories","classificationId":"` + clsServicio + `","categoryIds":[],"count":"all"},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.categoryIds", "required"},
		"categories unknown category":       {`{"config":{"selection":{"kind":"categories","classificationId":"` + clsServicio + `","categoryIds":["0abcdef0-0000-7000-8000-0000000cffff"],"count":"all"},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.categoryIds[0]", "not_found"},
		"categories unknown classification": {`{"config":{"selection":{"kind":"categories","classificationId":"0abcdef0-0000-7000-8000-0000000cffff","categoryIds":["` + catVPC + `"],"count":"all"},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.classificationId", "not_found"},
		"origin-set without set":            {`{"config":{"selection":{"kind":"origin-set"},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.set", "required"},
		"no timing":                         {`{"config":{"selection":{"kind":"random","count":5},"feedback":"end"}}`, "config.timing", "required"},
		"timing without kind":               {`{"config":{"selection":{"kind":"random","count":5},"timing":{},"feedback":"end"}}`, "config.timing.kind", "required"},
		"unknown timing kind":               {`{"config":{"selection":{"kind":"random","count":5},"timing":{"kind":"sometime"},"feedback":"end"}}`, "config.timing.kind", "format"},
		"timed without minutes":             {`{"config":{"selection":{"kind":"random","count":5},"timing":{"kind":"timed"},"feedback":"end"}}`, "config.timing.minutes", "required"},
		"timed 601 minutes":                 {`{"config":{"selection":{"kind":"random","count":5},"timing":{"kind":"timed","minutes":601},"feedback":"end"}}`, "config.timing.minutes", "range"},
		"timed 0 minutes":                   {`{"config":{"selection":{"kind":"random","count":5},"timing":{"kind":"timed","minutes":0},"feedback":"end"}}`, "config.timing.minutes", "range"},
		"no feedback":                       {`{"config":{"selection":{"kind":"random","count":5},"timing":{"kind":"untimed"}}}`, "config.feedback", "required"},
		"unknown feedback":                  {`{"config":{"selection":{"kind":"random","count":5},"timing":{"kind":"untimed"},"feedback":"later"}}`, "config.feedback", "format"},
		"negative seed":                     {`{"config":` + randomConfig + `,"seed":-1}`, "seed", "range"},
		"fractional seed":                   {`{"config":` + randomConfig + `,"seed":1.5}`, "seed", "range"},
		"string seed":                       {`{"config":` + randomConfig + `,"seed":"abc"}`, "seed", "range"},
		"seed above uint64":                 {`{"config":` + randomConfig + `,"seed":18446744073709551616}`, "seed", "range"},
		"not json":                          {`{"config":`, "body", "json"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := a.do(t, http.MethodPost, a.startPath(), tc.body)

			code, details := errorOf(t, rec)
			if rec.Code != http.StatusUnprocessableEntity || code != "validation_failed" || len(details) == 0 {
				t.Fatalf("status %d, code %s, details %v; want 422 validation_failed", rec.Code, code, details)
			}
			if details[0]["field"] != tc.field || details[0]["rule"] != tc.rule {
				t.Errorf("details[0] = %v, want %s/%s", details[0], tc.field, tc.rule)
			}
			if a.attempts.Count() != 0 {
				t.Error("a rejected start stored an attempt")
			}
		})
	}
}

func TestPreviewRequestValidation(t *testing.T) {
	a := newAttemptsAPI(t)
	for name, tc := range map[string]struct{ body, field, rule string }{
		"no config":      {`{}`, "config", "required"},
		"random count 0": {`{"config":{"selection":{"kind":"random","count":0},"timing":{"kind":"untimed"},"feedback":"end"}}`, "config.selection.count", "range"},
		"not json":       {`[`, "body", "json"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := a.do(t, http.MethodPost, a.previewPath(), tc.body)
			_, details := errorOf(t, rec)
			if rec.Code != http.StatusUnprocessableEntity || len(details) == 0 || details[0]["field"] != tc.field || details[0]["rule"] != tc.rule {
				t.Errorf("status %d, details %v; want 422 %s/%s", rec.Code, details, tc.field, tc.rule)
			}
		})
	}
}

func TestAnExplicitZeroSeedIsAccepted(t *testing.T) {
	a := newAttemptsAPI(t)
	if rec := a.start(t, randomConfig, "0"); rec.Code != http.StatusCreated {
		t.Errorf("status %d, body %s", rec.Code, rec.Body)
	}
}

func TestUnknownOrMalformedExamIdsAre404(t *testing.T) {
	a := newAttemptsAPI(t)
	for _, id := range []string{unknownID, "not-a-uuid"} {
		for _, path := range []string{examPath(id) + "/attempts", examPath(id) + "/sessions/preview"} {
			rec := a.do(t, http.MethodPost, path, `{"config":`+randomConfig+`}`)
			if code, _ := errorOf(t, rec); rec.Code != http.StatusNotFound || code != "not_found" {
				t.Errorf("POST %s: status %d code %s, want 404 not_found", path, rec.Code, code)
			}
		}
	}
}

func TestStartTranslatesBodyTooLargeTo413(t *testing.T) {
	a := newAttemptsAPI(t)
	huge := `{"config":` + randomConfig + `,"pad":"` + strings.Repeat("x", 2<<20) + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, a.startPath(), strings.NewReader(huge))
	a.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status %d, want 413", rec.Code)
	}
}
