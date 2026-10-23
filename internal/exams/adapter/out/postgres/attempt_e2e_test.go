package postgres

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	examshttp "github.com/afwilto/studies-mngr/internal/exams/adapter/in/http"
	"github.com/afwilto/studies-mngr/internal/exams/app/usecase"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

// The real stack of slice 06a: Postgres rows through the HTTP adapter.

type attemptE2E struct {
	*attemptEnv
	router http.Handler
}

// newAttemptE2E: an exam whose bank has 40 Seguridad and 40 Costos questions
// (Dominio, primary, inferred 50/50).
func newAttemptE2E(t *testing.T) *attemptE2E {
	t.Helper()
	b := newBankEnv(t)
	var questions []domain.FileQuestion
	for _, name := range []string{"Seguridad", "Costos"} {
		for i := range 40 {
			questions = append(questions, fq(fmt.Sprintf("%s-%02d", strings.ToLower(name), i), cn("Dominio", name)))
		}
	}
	b.apply(t, fileOf(questions...))
	e := &attemptEnv{bankEnv: b, attempts: NewAttemptRepository(pool)}
	deps := examshttp.Deps{
		PreviewSession: usecase.NewPreviewSessionService(b.exams, b.bank),
		StartAttempt: usecase.NewStartAttemptService(b.exams, b.bank, e.attempts, &testsupport.FixedClock{T: time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)},
			uuidGen{}, testsupport.FixedSeeds{Seed: 5}),
	}
	return &attemptE2E{attemptEnv: e, router: httpx.NewServer(httpx.Config{MaxBodyBytes: 1 << 20}, examshttp.Register(deps))}
}

func (e *attemptE2E) post(t *testing.T, path, body string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/exams/"+e.exam.ID+path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response %d: %s", rec.Code, rec.Body)
	}
	return rec.Code, out
}

func TestAttemptsEndToEndOverRealPostgres(t *testing.T) { // S06a-1, AC-14, AC-17, S06a-6 against the real bank and tables
	e := newAttemptE2E(t)
	const simulation = `{"config":{"selection":{"kind":"simulation"}},"seed":7}`

	status, preview := e.post(t, "/sessions/preview", `{"config":{"selection":{"kind":"simulation"}}}`)
	p, _ := preview["data"].(map[string]any)
	if status != http.StatusOK || p["poolSize"] != float64(80) || p["drawCount"] != float64(65) || p["suggestedMinutes"] != float64(130) {
		t.Fatalf("preview: %d %v", status, preview)
	}

	status, started := e.post(t, "/attempts", simulation)
	first, _ := started["data"].(map[string]any)
	if status != http.StatusCreated || len(first["items"].([]any)) != 65 {
		t.Fatalf("start: %d %v", status, started)
	}
	id := first["id"].(string)
	stored, err := loadAttempt(ctx, pool, id)
	if err != nil {
		t.Fatal(err)
	}
	perDomain := map[string]int{}
	for _, it := range stored.Items {
		perDomain[it.Snapshot.Categories[0].Category]++
	}
	// 65 over inferred 50/50: the tie's spare question goes to the first category
	if stored.Seed != 7 || stored.Status != domain.AttemptInProgress || perDomain["Seguridad"] != 33 || perDomain["Costos"] != 32 ||
		stored.DeadlineAt == nil || !stored.DeadlineAt.Equal(stored.StartedAt.Add(130*time.Minute)) || stored.SuggestedMinutes != 130 || stored.PassPercent != 72 {
		t.Errorf("stored attempt = %+v, per domain %v", stored, perDomain)
	}

	status, conflict := e.post(t, "/attempts", simulation)
	details := conflict["error"].(map[string]any)["details"].([]any)
	if status != http.StatusConflict || details[0].(map[string]any)["field"] != "attemptId" || !strings.Contains(details[0].(map[string]any)["message"].(string), id) {
		t.Errorf("second start: %d %v; want 409 naming %s", status, conflict, id)
	}

	exec(t, `UPDATE attempts SET status = 'submitted' WHERE id = $1`, id)
	status, again := e.post(t, "/attempts", simulation)
	if status != http.StatusCreated {
		t.Fatalf("start after closing the first: %d %v", status, again)
	}
	second, err := loadAttempt(ctx, pool, again["data"].(map[string]any)["id"].(string))
	if err != nil {
		t.Fatal(err)
	}
	for i := range stored.Items {
		a, b := stored.Items[i].Snapshot, second.Items[i].Snapshot
		if a.Key != b.Key || fmt.Sprint(a.Options) != fmt.Sprint(b.Options) {
			t.Fatalf("position %d differs with the same seed on an unchanged bank: %s vs %s", i, a.Key, b.Key)
		}
	}
}
