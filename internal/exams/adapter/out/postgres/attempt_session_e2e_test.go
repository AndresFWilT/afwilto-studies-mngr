package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	examshttp "github.com/afwilto/studies-mngr/internal/exams/adapter/in/http"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/app/usecase"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

// Slice 06b over the real stack: HTTP, use cases and Postgres, with a fake
// clock so the deadline is exact.

type sessionE2E struct {
	*attemptEnv
	router http.Handler
	clock  *testsupport.FixedClock
	t0     time.Time
}

func newSessionE2E(t *testing.T) *sessionE2E {
	t.Helper()
	b := newBankEnv(t)
	var questions []domain.FileQuestion
	for _, name := range []string{"Seguridad", "Costos"} {
		for i := range 40 {
			questions = append(questions, fq(fmt.Sprintf("%s-%02d", strings.ToLower(name), i), cn("Dominio", name)))
		}
	}
	for i := range 3 { // Timed Set 3, numbered 1..3, all pointing at the Route 53 note
		q := fq(fmt.Sprintf("ts3-%d", i), cn("Dominio", "Seguridad"))
		q.Explanation = fmt.Sprintf("explanation %d", i)
		q.Origin = &domain.Origin{Set: "Timed Set 3", Number: i + 1, NoteRef: "03 · Route 53 (diap. 12)"}
		questions = append(questions, q)
	}
	b.apply(t, fileOf(questions...))

	e := &attemptEnv{bankEnv: b, attempts: NewAttemptRepository(pool)}
	t0 := time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)
	clock := &testsupport.FixedClock{T: t0}
	notes := NewNoteDirectory(pool)
	deps := examshttp.Deps{
		GetExam:          usecase.NewGetExamService(b.exams),
		PreviewSession:   usecase.NewPreviewSessionService(b.exams, b.bank),
		StartAttempt:     usecase.NewStartAttemptService(b.exams, b.bank, e.attempts, clock, uuidGen{}, testsupport.FixedSeeds{Seed: 5}),
		GetAttempt:       usecase.NewGetAttemptService(e.attempts, clock),
		AnswerItem:       usecase.NewAnswerItemService(e.attempts, clock),
		SubmitAttempt:    usecase.NewSubmitAttemptService(e.attempts, clock),
		ReviewAttempt:    usecase.NewReviewAttemptService(e.attempts, notes, clock),
		ListExamAttempts: usecase.NewListExamAttemptsService(b.exams, e.attempts),
	}
	return &sessionE2E{attemptEnv: e, clock: clock, t0: t0, router: httpx.NewServer(httpx.Config{MaxBodyBytes: 1 << 20}, examshttp.Register(deps))}
}

func (e *sessionE2E) call(t *testing.T, method, path, body string) (int, map[string]any) {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, nil)
	} else {
		req = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("%s %s -> %d: %s", method, path, rec.Code, rec.Body)
	}
	return rec.Code, out
}

func (e *sessionE2E) start(t *testing.T, config string) map[string]any {
	t.Helper()
	status, out := e.call(t, http.MethodPost, "/api/v1/exams/"+e.exam.ID+"/attempts", `{"config":`+config+`,"seed":7}`)
	if status != http.StatusCreated {
		t.Fatalf("start: %d %v", status, out)
	}
	return out["data"].(map[string]any)
}

func (e *sessionE2E) put(t *testing.T, id string, position int, flagged bool, labels ...string) (int, map[string]any) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"selected": append([]string{}, labels...), "flagged": flagged})
	return e.call(t, http.MethodPut, fmt.Sprintf("/api/v1/attempts/%s/items/%d", id, position), string(body))
}

func (e *sessionE2E) data(t *testing.T, method, path string) map[string]any {
	t.Helper()
	status, out := e.call(t, method, path, "")
	if status != http.StatusOK {
		t.Fatalf("%s %s: %d %v", method, path, status, out)
	}
	return out["data"].(map[string]any)
}

// rightLabel is the label shown for the option "right one", the fixture's key.
func rightLabel(item any) string {
	for _, o := range item.(map[string]any)["options"].([]any) {
		if opt := o.(map[string]any); opt["text"] == "right one" {
			return opt["label"].(string)
		}
	}
	return ""
}

func wrongLabel(item any) string {
	for _, o := range item.(map[string]any)["options"].([]any) {
		if opt := o.(map[string]any); opt["text"] == "wrong one" {
			return opt["label"].(string)
		}
	}
	return ""
}

const simConfig = `{"selection":{"kind":"simulation"}}`

func TestASimulationFromStartToLastSimulationOverRealPostgres(t *testing.T) { // S06b-4, §1.7, FR-10
	e := newSessionE2E(t)
	started := e.start(t, simConfig)
	id := started["id"].(string)
	items := started["items"].([]any)

	for i := 0; i < 47; i++ { // 47 of 65 = 72.31%
		if status, out := e.put(t, id, i, i%10 == 0, rightLabel(items[i])); status != http.StatusOK {
			t.Fatalf("PUT %d: %d %v", i, status, out)
		}
	}
	if status, out := e.put(t, id, 47, false, wrongLabel(items[47])); status != http.StatusOK {
		t.Fatalf("PUT 47: %d %v", status, out)
	}
	e.clock.Advance(95 * time.Minute)

	status, out := e.call(t, http.MethodPost, "/api/v1/attempts/"+id+"/submit", "")
	result, _ := out["data"].(map[string]any)
	if status != http.StatusOK || result["scorePercent"] != 72.31 || result["passed"] != true || result["correctCount"] != float64(47) ||
		result["total"] != float64(65) || result["durationSeconds"] != float64(5700) {
		t.Fatalf("submit: %d %v", status, out)
	}

	var score float64
	var passed bool
	var correct, revealed int
	if err := pool.QueryRow(ctx, `SELECT score_percent::float8, passed FROM attempts WHERE id = $1`, id).Scan(&score, &passed); err != nil || score != 72.31 || !passed {
		t.Errorf("row: score %v passed %v err %v", score, passed, err)
	}
	_ = pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE correct), count(*) FILTER (WHERE revealed) FROM attempt_items WHERE attempt_id = $1`, id).Scan(&correct, &revealed)
	if correct != 47 || revealed != 0 {
		t.Errorf("stored correct flags = %d (want 47), revealed = %d (want 0 under feedback end)", correct, revealed)
	}

	exam := e.data(t, http.MethodGet, "/api/v1/exams/"+e.exam.ID)
	last, _ := exam["lastSimulation"].(map[string]any)
	if last["scorePercent"] != 72.31 || last["passed"] != true || last["at"] != "2026-10-23T10:35:00Z" {
		t.Errorf("lastSimulation = %v, want 72.31 passed at 10:35", exam["lastSimulation"])
	}

	got := e.data(t, http.MethodGet, "/api/v1/attempts/"+id)
	if got["status"] != "submitted" || fmt.Sprint(got["result"]) != fmt.Sprint(result) {
		t.Errorf("GET after submit = %v, want the submit's result", got["result"])
	}
}

func TestLazyExpiryAndStartFinalizingOverRealPostgres(t *testing.T) { // S06b-1, AC-19, S06b-8, decision 3
	e := newSessionE2E(t)
	timed := `{"selection":{"kind":"random","count":4},"timing":{"kind":"timed","minutes":10},"feedback":"end"}`
	started := e.start(t, timed)
	id := started["id"].(string)
	items := started["items"].([]any)
	e.put(t, id, 0, false, rightLabel(items[0]))

	e.clock.T = e.t0.Add(10*time.Minute - 10*time.Second)
	if got := e.data(t, http.MethodGet, "/api/v1/attempts/"+id); got["remainingSeconds"] != float64(10) {
		t.Errorf("T-10 s: %v", got["remainingSeconds"])
	}
	e.clock.T = e.t0.Add(10*time.Minute + 3*time.Second)
	if got := e.data(t, http.MethodGet, "/api/v1/attempts/"+id); got["remainingSeconds"] != float64(0) || got["status"] != "in_progress" {
		t.Errorf("T+3 s: %v %v", got["remainingSeconds"], got["status"])
	}
	if status, _ := e.put(t, id, 1, false, rightLabel(items[1])); status != http.StatusOK {
		t.Errorf("a PUT inside the grace: %d", status)
	}
	e.clock.T = e.t0.Add(10*time.Minute + 6*time.Second)

	status, out := e.put(t, id, 2, false, rightLabel(items[2]))
	rule := out["error"].(map[string]any)["details"].([]any)[0].(map[string]any)["rule"]
	if status != http.StatusConflict || rule != "attempt_expired" {
		t.Fatalf("late PUT: %d %v", status, out)
	}
	var state string
	var score float64
	var submittedAt time.Time
	if err := pool.QueryRow(ctx, `SELECT status, score_percent::float8, submitted_at FROM attempts WHERE id = $1`, id).Scan(&state, &score, &submittedAt); err != nil ||
		state != "expired" || score != 50 || !submittedAt.Equal(e.t0.Add(10*time.Minute)) {
		t.Errorf("row after the late PUT: %q score %v submittedAt %v err %v; want expired, 50, at the deadline", state, score, submittedAt, err)
	}
	if status, out := e.call(t, http.MethodPost, "/api/v1/attempts/"+id+"/submit", ""); status != http.StatusConflict ||
		out["error"].(map[string]any)["details"].([]any)[0].(map[string]any)["rule"] != "attempt_closed" {
		t.Errorf("submit after the expiry: %d %v", status, out)
	}

	// Decision 3: an open attempt past its grace does not block the next start.
	second := e.start(t, timed)
	e.clock.Advance(time.Hour)
	third := e.start(t, `{"selection":{"kind":"random","count":2},"timing":{"kind":"untimed"},"feedback":"end"}`)
	if third["id"] == second["id"] {
		t.Fatal("the third start returned the second attempt")
	}
	if err := pool.QueryRow(ctx, `SELECT status FROM attempts WHERE id = $1`, second["id"]).Scan(&state); err != nil || state != "expired" {
		t.Errorf("second attempt = %q (err %v), want expired by the start that replaced it", state, err)
	}
}

func TestFeedbackEachStoresTheRevealAndLockOverRealPostgres(t *testing.T) { // S06b-2, D-13
	e := newSessionE2E(t)
	started := e.start(t, `{"selection":{"kind":"origin-set","set":"Timed Set 3"},"timing":{"kind":"untimed"},"feedback":"each"}`)
	id := started["id"].(string)
	first := started["items"].([]any)[0]

	status, out := e.put(t, id, 0, false, wrongLabel(first))
	reveal := out["data"].(map[string]any)
	if status != http.StatusOK || reveal["correct"] != false || fmt.Sprint(reveal["correctLabels"]) != fmt.Sprint([]string{rightLabel(first)}) {
		t.Fatalf("PUT: %d %v", status, out)
	}
	if status, out = e.put(t, id, 0, false, rightLabel(first)); status != http.StatusConflict {
		t.Errorf("changing a revealed answer: %d %v, want 409", status, out)
	}
	if status, _ = e.put(t, id, 0, true, wrongLabel(first)); status != http.StatusOK {
		t.Errorf("flag-only change: %d", status)
	}
	var revealed, flagged bool
	var correct *bool
	if err := pool.QueryRow(ctx, `SELECT revealed, flagged, correct FROM attempt_items WHERE attempt_id = $1 AND position = 0`, id).Scan(&revealed, &flagged, &correct); err != nil ||
		!revealed || !flagged || correct == nil || *correct {
		t.Errorf("stored item: revealed %v flagged %v correct %v err %v", revealed, flagged, correct, err)
	}
	items := e.data(t, http.MethodGet, "/api/v1/attempts/"+id)["items"].([]any)
	if _, has := items[1].(map[string]any)["correctLabels"]; has {
		t.Error("an unanswered item shows the answer key")
	}
}

func TestReviewLinksTheNoteFromTheRealNotesTableUntilItIsDeleted(t *testing.T) { // S06b-6
	e := newSessionE2E(t)
	noteID := e.note(t, e.subjectID, "03 · Route 53")
	started := e.start(t, `{"selection":{"kind":"origin-set","set":"Timed Set 3"},"timing":{"kind":"untimed"},"feedback":"end"}`)
	id := started["id"].(string)
	e.call(t, http.MethodPost, "/api/v1/attempts/"+id+"/submit", "")

	review := func() []any {
		status, out := e.call(t, http.MethodGet, "/api/v1/attempts/"+id+"/review", "")
		if status != http.StatusOK {
			t.Fatalf("review: %d %v", status, out)
		}
		return out["data"].([]any)
	}
	for i, it := range review() {
		if it.(map[string]any)["noteId"] != noteID {
			t.Errorf("item %d noteId = %v, want %s", i, it.(map[string]any)["noteId"], noteID)
		}
	}
	exec(t, `UPDATE notes SET deleted_at = now() WHERE id = $1`, noteID)
	for i, it := range review() {
		if _, has := it.(map[string]any)["noteId"]; has {
			t.Errorf("item %d still links a deleted note", i)
		}
	}
}

func TestConcurrentSubmitsOverHTTPCloseTheAttemptOnce(t *testing.T) { // the row lock end to end
	e := newSessionE2E(t)
	id := e.start(t, `{"selection":{"kind":"random","count":3},"timing":{"kind":"untimed"},"feedback":"end"}`)["id"].(string)
	const racers = 6
	statuses := make([]int, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/attempts/"+id+"/submit", nil)
			rec := httptest.NewRecorder()
			e.router.ServeHTTP(rec, req)
			statuses[i] = rec.Code
		}()
	}
	wg.Wait()

	ok, conflict := 0, 0
	for _, s := range statuses {
		switch s {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			conflict++
		}
	}
	if ok != 1 || conflict != racers-1 {
		t.Errorf("statuses = %v, want one 200 and %d 409", statuses, racers-1)
	}
}

// ── phase-3 fix 2: start racing a concurrent close of the open attempt ──────

// afterFindRepo runs hook right after FindInProgress found an open attempt,
// the window in which another request closes it.
type afterFindRepo struct {
	out.AttemptRepository
	hook func()
}

func (r afterFindRepo) FindInProgress(ctx context.Context, examID string) (string, bool, error) {
	id, found, err := r.AttemptRepository.FindInProgress(ctx, examID)
	if found && r.hook != nil {
		r.hook()
	}
	return id, found, err
}

func (e *sessionE2E) startService(hook func()) in.StartAttempt {
	return usecase.NewStartAttemptService(e.exams, e.bank, afterFindRepo{AttemptRepository: e.attempts, hook: hook}, e.clock, uuidGen{}, testsupport.FixedSeeds{Seed: 5})
}

func (e *sessionE2E) inProgressRows(t *testing.T) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `SELECT id FROM attempts WHERE exam_id = $1 AND status = 'in_progress'`, e.exam.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

func untimedTwo() domain.SessionConfig {
	return domain.SessionConfig{Selection: domain.Selection{Kind: domain.SelectRandom, Count: 2}, Timing: &domain.Timing{Kind: domain.Untimed}, Feedback: domain.FeedbackEnd}
}

func TestStartRacingASubmitOfTheOpenAttemptNeverNamesAClosedAttemptOverRealPostgres(t *testing.T) { // phase-3 fix 2
	e := newSessionE2E(t)
	open := e.start(t, `{"selection":{"kind":"random","count":3},"timing":{"kind":"untimed"},"feedback":"end"}`)["id"].(string)
	start := e.startService(func() { e.call(t, http.MethodPost, "/api/v1/attempts/"+open+"/submit", "") })

	fresh, err := start.Execute(ctx, e.exam.ID, in.StartAttemptCommand{Config: untimedTwo()})

	if err != nil || fresh.ID == open {
		t.Fatalf("start = %v, err %v; want a new attempt, not a 409 naming the submitted %s", fresh.ID, err, open)
	}
	if rows := e.inProgressRows(t); len(rows) != 1 || rows[0] != fresh.ID {
		t.Errorf("in_progress rows = %v, want exactly the new attempt %s", rows, fresh.ID)
	}
}

func TestStartRacingAFinalizeOfTheExpiredAttemptNeverNamesAClosedAttemptOverRealPostgres(t *testing.T) { // phase-3 fix 2
	e := newSessionE2E(t)
	old := e.start(t, `{"selection":{"kind":"random","count":3},"timing":{"kind":"timed","minutes":10},"feedback":"end"}`)["id"].(string)
	e.clock.T = e.t0.Add(10*time.Minute + 6*time.Second)
	start := e.startService(func() { e.call(t, http.MethodGet, "/api/v1/attempts/"+old, "") })

	fresh, err := start.Execute(ctx, e.exam.ID, in.StartAttemptCommand{Config: untimedTwo()})

	if err != nil || fresh.ID == old {
		t.Fatalf("start = %v, err %v; want a new attempt, not a 409 naming the expired %s", fresh.ID, err, old)
	}
	if rows := e.inProgressRows(t); len(rows) != 1 || rows[0] != fresh.ID {
		t.Errorf("in_progress rows = %v, want exactly the new attempt %s", rows, fresh.ID)
	}
}

func TestStartsRacingFinalizingReadsAndWritesOfAnExpiredAttemptAlwaysSucceedOverRealPostgres(t *testing.T) { // phase-3 fix 2, unhooked
	e := newSessionE2E(t)
	for round := range 8 {
		old := e.start(t, `{"selection":{"kind":"random","count":3},"timing":{"kind":"timed","minutes":10},"feedback":"end"}`)["id"].(string)
		e.clock.Advance(11 * time.Minute)
		var wg sync.WaitGroup
		var startErr error
		var fresh domain.AttemptView
		wg.Add(3)
		go func() { defer wg.Done(); e.call(t, http.MethodGet, "/api/v1/attempts/"+old, "") }()
		go func() { defer wg.Done(); e.put(t, old, 0, true) }()
		go func() {
			defer wg.Done()
			fresh, startErr = e.startService(nil).Execute(ctx, e.exam.ID, in.StartAttemptCommand{Config: untimedTwo()})
		}()
		wg.Wait()

		if startErr != nil {
			t.Fatalf("round %d: start = %v; an attempt past its grace never blocks a start", round, startErr)
		}
		if rows := e.inProgressRows(t); len(rows) != 1 || rows[0] != fresh.ID {
			t.Fatalf("round %d: in_progress rows = %v, want exactly %s", round, rows, fresh.ID)
		}
		e.call(t, http.MethodPost, "/api/v1/attempts/"+fresh.ID+"/submit", "")
	}
}
