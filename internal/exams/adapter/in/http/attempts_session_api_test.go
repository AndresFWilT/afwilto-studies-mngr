package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

// Slice 06b over HTTP: the fake clock, fake repositories and the real use
// cases, with every response validated against openapi.yaml by apiEnv.do.

func (a attemptsAPI) at(d time.Duration) { a.clock.T = attemptsT0.Add(d) }

func (a attemptsAPI) attemptPath(id string) string { return "/api/v1/attempts/" + id }

func (a attemptsAPI) get(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()
	return a.do(t, http.MethodGet, a.attemptPath(id), "")
}

func (a attemptsAPI) put(t *testing.T, id string, position int, body string) *httptest.ResponseRecorder {
	t.Helper()
	return a.do(t, http.MethodPut, fmt.Sprintf("%s/items/%d", a.attemptPath(id), position), body)
}

func (a attemptsAPI) answer(t *testing.T, id string, position int, flagged bool, labels ...string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"selected": append([]string{}, labels...), "flagged": flagged})
	return a.put(t, id, position, string(body))
}

func (a attemptsAPI) mustAnswer(t *testing.T, id string, position int, flagged bool, labels ...string) map[string]any {
	t.Helper()
	rec := a.answer(t, id, position, flagged, labels...)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT %d %v: status %d, body %s", position, labels, rec.Code, rec.Body)
	}
	return decodeData(t, rec)
}

func (a attemptsAPI) submit(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()
	return a.do(t, http.MethodPost, a.attemptPath(id)+"/submit", "")
}

func (a attemptsAPI) mustGet(t *testing.T, id string) map[string]any {
	t.Helper()
	rec := a.get(t, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET: status %d, body %s", rec.Code, rec.Body)
	}
	return decodeData(t, rec)
}

func (a attemptsAPI) mustSubmit(t *testing.T, id string) map[string]any {
	t.Helper()
	rec := a.submit(t, id)
	if rec.Code != http.StatusOK {
		t.Fatalf("submit: status %d, body %s", rec.Code, rec.Body)
	}
	return decodeData(t, rec)
}

func (a attemptsAPI) review(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()
	return a.do(t, http.MethodGet, a.attemptPath(id)+"/review", "")
}

func (a attemptsAPI) history(t *testing.T) *httptest.ResponseRecorder {
	t.Helper()
	return a.do(t, http.MethodGet, examPath(a.examID)+"/attempts", "")
}

// labelOf is the label the item shows for the option whose text is given; the
// fixture's option "<key>/b" is the correct one.
func labelOf(t *testing.T, item map[string]any, text string) string {
	t.Helper()
	for _, o := range item["options"].([]any) {
		opt := o.(map[string]any)
		if opt["text"] == text {
			return opt["label"].(string)
		}
	}
	t.Fatalf("item has no option %q: %v", text, item)
	return ""
}

func keyOf(t *testing.T, item map[string]any) string {
	t.Helper()
	return labelOf(t, item, strings.TrimPrefix(item["stem"].(string), "stem ")+"/b")
}

func wrongOf(t *testing.T, item map[string]any) string {
	t.Helper()
	return labelOf(t, item, strings.TrimPrefix(item["stem"].(string), "stem ")+"/a")
}

const (
	timedEnd   = `{"selection":{"kind":"random","count":4},"timing":{"kind":"timed","minutes":10},"feedback":"end"}`
	untimedEnd = `{"selection":{"kind":"random","count":4},"timing":{"kind":"untimed"},"feedback":"end"}`
)

var revealKeys = []string{"correct", "correctLabels", "rationales", "explanation"}

// ── AC-18, BR-A4: nothing reveals correctness while the attempt is open ─────

func TestFeedbackEndNeverRevealsCorrectnessInAnyResponseWhileOpen(t *testing.T) { // AC-18, BR-A4
	a := newAttemptsAPI(t)
	start := a.mustStart(t, timedEnd, "")
	id := start["id"].(string)
	items := itemsOf(t, start)

	var responses []*httptest.ResponseRecorder
	responses = append(responses,
		a.answer(t, id, 0, false, keyOf(t, items[0])),
		a.answer(t, id, 1, true, wrongOf(t, items[1])),
		a.answer(t, id, 1, false),
		a.answer(t, id, 2, true, keyOf(t, items[2])),
		a.get(t, id))

	for i, rec := range responses {
		if rec.Code != http.StatusOK {
			t.Fatalf("response %d: status %d, body %s", i, rec.Code, rec.Body)
		}
		var doc any
		if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
			t.Fatal(err)
		}
		seen := 0
		walkKeys(doc, func(key string) {
			seen++
			for _, banned := range revealKeys {
				if key == banned {
					t.Errorf("response %d has the key %q while the attempt is open and feedback is end", i, key)
				}
			}
		})
		if seen < 4 {
			t.Errorf("response %d: the walk saw %d keys", i, seen)
		}
		if body := rec.Body.String(); strings.Contains(body, "why ") || strings.Contains(body, "explanation of") {
			t.Errorf("response %d leaks rationale or explanation text: %.200s", i, body)
		}
	}
}

func TestPutWithFeedbackEndReturnsExactlyPositionSelectedFlaggedAndTheClock(t *testing.T) { // AC-18, decision 2
	a := newAttemptsAPI(t)
	timed := a.mustStart(t, timedEnd, "")
	a.at(time.Minute)

	data := a.mustAnswer(t, timed["id"].(string), 2, true, "C")

	want := map[string]any{"position": float64(2), "selected": []any{"C"}, "flagged": true, "remainingSeconds": float64(540)}
	if !reflect.DeepEqual(data, want) {
		t.Errorf("PUT on a timed attempt = %v, want %v", data, want)
	}
}

func TestPutOnAnUntimedAttemptHasNoRemainingSeconds(t *testing.T) { // decision 2: omitted for untimed
	a := newAttemptsAPI(t)
	start := a.mustStart(t, untimedEnd, "")

	data := a.mustAnswer(t, start["id"].(string), 0, false, "A")

	want := map[string]any{"position": float64(0), "selected": []any{"A"}, "flagged": false}
	if !reflect.DeepEqual(data, want) {
		t.Errorf("PUT on an untimed attempt = %v, want exactly %v", data, want)
	}
}

// ── S06b-1, AC-19: the deadline over HTTP ──────────────────────────────────

func TestGetCountsDownHoldsInTheGraceAndExpiresWithAScoreOverHTTP(t *testing.T) { // S06b-1
	a := newAttemptsAPI(t)
	start := a.mustStart(t, timedEnd, "")
	id := start["id"].(string)
	a.mustAnswer(t, id, 0, false, keyOf(t, itemsOf(t, start)[0]))

	a.at(10*time.Minute - 10*time.Second)
	if d := a.mustGet(t, id); d["remainingSeconds"] != float64(10) || d["status"] != "in_progress" {
		t.Errorf("T-10 s: remaining %v, status %v; want 10, in_progress", d["remainingSeconds"], d["status"])
	}
	a.at(10*time.Minute + 3*time.Second)
	if d := a.mustGet(t, id); d["remainingSeconds"] != float64(0) || d["status"] != "in_progress" {
		t.Errorf("T+3 s: remaining %v, status %v; want 0, in_progress", d["remainingSeconds"], d["status"])
	}
	if _, has := a.mustGet(t, id)["result"]; has {
		t.Error("an attempt still in its grace period has a result")
	}
	a.at(10*time.Minute + 6*time.Second)
	d := a.mustGet(t, id)
	result, _ := d["result"].(map[string]any)
	if d["status"] != "expired" || result == nil || result["scorePercent"] != float64(25) || result["correctCount"] != float64(1) || result["total"] != float64(4) {
		t.Errorf("T+6 s: status %v, result %v; want expired with 25%% (1 of 4)", d["status"], d["result"])
	}
	if d["remainingSeconds"] != float64(0) {
		t.Errorf("remaining of the expired attempt = %v, want 0", d["remainingSeconds"])
	}
}

func TestAPutFourSecondsLateIsAcceptedAndOneSixSecondsLateIsAttemptExpired(t *testing.T) { // AC-19
	a := newAttemptsAPI(t)
	start := a.mustStart(t, timedEnd, "")
	id := start["id"].(string)
	items := itemsOf(t, start)

	a.at(10*time.Minute + 4*time.Second)
	a.mustAnswer(t, id, 0, false, keyOf(t, items[0]))

	a.at(10*time.Minute + 6*time.Second)
	rec := a.answer(t, id, 1, false, keyOf(t, items[1]))
	code, details := errorOf(t, rec)
	if rec.Code != http.StatusConflict || code != "conflict" || len(details) != 1 || details[0]["rule"] != "attempt_expired" {
		t.Fatalf("status %d, %s %v; want 409 conflict attempt_expired", rec.Code, code, details)
	}

	d := a.mustGet(t, id)
	if d["status"] != "expired" || d["result"].(map[string]any)["scorePercent"] != float64(25) {
		t.Errorf("after the late PUT: status %v, result %v; want expired and scored", d["status"], d["result"])
	}
	rec = a.answer(t, id, 1, false, "A")
	if _, details = errorOf(t, rec); rec.Code != http.StatusConflict || details[0]["rule"] != "attempt_closed" {
		t.Errorf("a PUT on the already-expired attempt: %d %v, want 409 attempt_closed", rec.Code, details)
	}
}

func TestSubmitAfterTheExpiryIsAttemptClosedAndTheAttemptIsAlreadyScored(t *testing.T) { // S06b-8
	a := newAttemptsAPI(t)
	start := a.mustStart(t, timedEnd, "")
	id := start["id"].(string)
	a.mustAnswer(t, id, 0, false, keyOf(t, itemsOf(t, start)[0]))
	a.at(10*time.Minute + 6*time.Second)

	rec := a.submit(t, id)

	if _, details := errorOf(t, rec); rec.Code != http.StatusConflict || details[0]["rule"] != "attempt_closed" {
		t.Fatalf("status %d %v, want 409 attempt_closed", rec.Code, details)
	}
	if d := a.mustGet(t, id); d["status"] != "expired" || d["result"].(map[string]any)["scorePercent"] != float64(25) {
		t.Errorf("attempt = %v, want expired and scored", d)
	}
}

// ── GET: the answer state and the reveal (decision 1, S06b-2) ──────────────

func TestGetAddsSelectedFlaggedAndAnsweredToEveryItem(t *testing.T) {
	a := newAttemptsAPI(t)
	start := a.mustStart(t, untimedEnd, "")
	id := start["id"].(string)
	a.mustAnswer(t, id, 1, true, "C")
	a.mustAnswer(t, id, 2, true)

	items := itemsOf(t, a.mustGet(t, id))

	want := []map[string]any{
		{"selected": []any{}, "flagged": false, "answered": false},
		{"selected": []any{"C"}, "flagged": true, "answered": true},
		{"selected": []any{}, "flagged": true, "answered": false},
		{"selected": []any{}, "flagged": false, "answered": false},
	}
	for i, it := range items {
		got := map[string]any{"selected": it["selected"], "flagged": it["flagged"], "answered": it["answered"]}
		if !reflect.DeepEqual(got, want[i]) {
			t.Errorf("item %d = %v, want %v", i, got, want[i])
		}
	}
}

func TestFeedbackEachRevealsAWrongAnswerLocksItAndStillTakesTheFlagOverHTTP(t *testing.T) { // S06b-2
	a := newAttemptsAPI(t)
	start := a.mustStart(t, originSetConfig, "") // Timed Set 3 numbered 1, 2, 3: ts3-a, ts3-b, ts3-m
	id := start["id"].(string)
	first := itemsOf(t, start)[0]
	wrong, key := wrongOf(t, first), keyOf(t, first)

	data := a.mustAnswer(t, id, 0, false, wrong)

	if data["correct"] != false || !reflect.DeepEqual(data["correctLabels"], []any{key}) || data["explanation"] != "explanation of ts3-a" {
		t.Fatalf("reveal = %v, want wrong with correctLabels [%s] and the explanation", data, key)
	}
	rationales, _ := data["rationales"].(map[string]any)
	if len(rationales) != 4 || rationales[key] != "why ts3-a/b" {
		t.Errorf("rationales = %v, want 4 entries keyed by label", rationales)
	}

	rec := a.answer(t, id, 0, false, key)
	if _, details := errorOf(t, rec); rec.Code != http.StatusConflict || details[0]["rule"] != "answer_locked" {
		t.Errorf("a different selection: %d %v, want 409 answer_locked", rec.Code, details)
	}
	rec = a.answer(t, id, 0, true, wrong)
	flagOnly := decodeData(t, rec)
	if rec.Code != http.StatusOK || flagOnly["flagged"] != true || flagOnly["correct"] != false {
		t.Errorf("flag-only change: %d %v, want 200 flagged with the reveal repeated", rec.Code, flagOnly)
	}

	items := itemsOf(t, a.mustGet(t, id))
	if items[0]["correct"] != false || !reflect.DeepEqual(items[0]["correctLabels"], []any{key}) || items[0]["explanation"] != "explanation of ts3-a" {
		t.Errorf("GET item 0 = %v, want the reveal of the revealed item", items[0])
	}
	for _, it := range items[1:] {
		for _, banned := range revealKeys {
			if _, has := it[banned]; has {
				t.Errorf("GET item %v has %q although it was never answered", it["position"], banned)
			}
		}
	}
}

func TestFeedbackEachWithAnEmptySelectionRevealsNothingOverHTTP(t *testing.T) {
	a := newAttemptsAPI(t)
	id := a.mustStart(t, originSetConfig, "")["id"].(string)

	data := a.mustAnswer(t, id, 0, true)

	for _, banned := range revealKeys {
		if _, has := data[banned]; has {
			t.Errorf("an empty selection revealed %q", banned)
		}
	}
}

// ── validation over HTTP (S06b-3, AC-20) ───────────────────────────────────

func TestPutOfALabelTheItemDoesNotShowIsInvalidLabel(t *testing.T) { // S06b-3
	a := newAttemptsAPI(t)
	id := a.mustStart(t, untimedEnd, "")["id"].(string)

	rec := a.answer(t, id, 0, false, "Z")

	code, details := errorOf(t, rec)
	if rec.Code != http.StatusUnprocessableEntity || code != "validation_failed" || len(details) != 1 || details[0]["rule"] != "invalid_label" || details[0]["field"] != "selected[0]" {
		t.Errorf("status %d, %s %v; want 422 invalid_label on selected[0]", rec.Code, code, details)
	}
}

func TestPutOfARepeatedLabelIsInvalidLabel(t *testing.T) {
	a := newAttemptsAPI(t)
	id := a.mustStart(t, originSetConfig, "")["id"].(string) // item 2 is the multi one

	rec := a.answer(t, id, 2, false, "A", "A")

	if _, details := errorOf(t, rec); rec.Code != http.StatusUnprocessableEntity || details[0]["rule"] != "invalid_label" || details[0]["field"] != "selected[1]" {
		t.Errorf("status %d %v, want 422 invalid_label on selected[1]", rec.Code, details)
	}
}

func TestPutOfMoreLabelsThanChooseCountIsTooManySelected(t *testing.T) { // AC-20, BR-A5
	a := newAttemptsAPI(t)
	id := a.mustStart(t, originSetConfig, "")["id"].(string) // item 2 is multi with chooseCount 2

	for name, labels := range map[string][]string{"multi over its count": {"A", "B", "D"}} {
		rec := a.answer(t, id, 2, false, labels...)
		if _, details := errorOf(t, rec); rec.Code != http.StatusUnprocessableEntity || details[0]["rule"] != "too_many_selected" {
			t.Errorf("%s: status %d %v, want 422 too_many_selected", name, rec.Code, details)
		}
	}
	a.mustAnswer(t, id, 2, false, "B", "D")
	rec := a.answer(t, id, 0, false, "A", "B") // a single item takes one
	if _, details := errorOf(t, rec); rec.Code != http.StatusUnprocessableEntity || details[0]["rule"] != "too_many_selected" {
		t.Errorf("single with two labels: status %d %v, want 422 too_many_selected", rec.Code, details)
	}
}

func TestPutWithAMalformedBodyIsAValidationFailure(t *testing.T) {
	a := newAttemptsAPI(t)
	id := a.mustStart(t, untimedEnd, "")["id"].(string)

	for name, tc := range map[string]struct{ body, field, rule string }{
		"selected missing":    {`{"flagged":false}`, "selected", "required"},
		"selected null":       {`{"selected":null,"flagged":false}`, "selected", "required"},
		"flagged missing":     {`{"selected":["A"]}`, "flagged", "required"},
		"flagged null":        {`{"selected":["A"],"flagged":null}`, "flagged", "required"},
		"not json":            {`{"selected":`, "body", "json"},
		"selected not labels": {`{"selected":[1],"flagged":false}`, "body", "json"},
	} {
		rec := a.put(t, id, 0, tc.body)
		code, details := errorOf(t, rec)
		if rec.Code != http.StatusUnprocessableEntity || code != "validation_failed" || len(details) == 0 || details[0]["field"] != tc.field || details[0]["rule"] != tc.rule {
			t.Errorf("%s: status %d, %s %v; want 422 %s/%s", name, rec.Code, code, details, tc.field, tc.rule)
		}
	}
}

func TestPutToAnUnknownAttemptOrPositionIs404(t *testing.T) {
	a := newAttemptsAPI(t)
	id := a.mustStart(t, untimedEnd, "")["id"].(string)
	body := `{"selected":["A"],"flagged":false}`

	for name, path := range map[string]string{
		"unknown attempt":  "/api/v1/attempts/0abcdef0-0000-7000-8000-ffffffffffff/items/0",
		"position too big": a.attemptPath(id) + "/items/4",
		"negative":         a.attemptPath(id) + "/items/-1",
		"not a number":     a.attemptPath(id) + "/items/first",
		"bad attempt id":   "/api/v1/attempts/nope/items/0",
	} {
		if rec := a.do(t, http.MethodPut, path, body); rec.Code != http.StatusNotFound {
			t.Errorf("%s: status %d, want 404", name, rec.Code)
		}
	}
}

func TestAttemptReadsOfAnUnknownIdAre404(t *testing.T) {
	a := newAttemptsAPI(t)
	ghost := a.attemptPath("0abcdef0-0000-7000-8000-ffffffffffff")
	for _, tc := range []struct{ method, path string }{
		{http.MethodGet, ghost}, {http.MethodPost, ghost + "/submit"}, {http.MethodGet, ghost + "/review"},
		{http.MethodGet, a.attemptPath("nope")}, {http.MethodGet, examPath("0abcdef0-0000-7000-8000-ffffffffffff") + "/attempts"},
	} {
		if rec := a.do(t, tc.method, tc.path, ""); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s: status %d, want 404", tc.method, tc.path, rec.Code)
		}
	}
}

// ── submit, result ──────────────────────────────────────────────────────────

func TestSubmitScoresFiftyPercentWithNullPassedAndTheWeakestCategoryFirst(t *testing.T) { // S06b-4, S06b-5
	a := newAttemptsAPI(t)
	start := a.mustStart(t, untimedEnd, "")
	id := start["id"].(string)
	items := itemsOf(t, start)
	a.mustAnswer(t, id, 0, false, keyOf(t, items[0]))
	a.mustAnswer(t, id, 1, false, keyOf(t, items[1]))
	a.mustAnswer(t, id, 2, false, wrongOf(t, items[2]))
	a.at(12 * time.Minute)

	result := a.mustSubmit(t, id)

	if result["scorePercent"] != float64(50) || result["correctCount"] != float64(2) || result["total"] != float64(4) || result["durationSeconds"] != float64(720) {
		t.Errorf("result = %v, want 50%%, 2 of 4, 720 s", result)
	}
	if passed, has := result["passed"]; !has || passed != nil {
		t.Errorf("passed = %v (present %v), want an explicit null for a random attempt", passed, has)
	}
	breakdown, _ := result["breakdown"].([]any)
	if len(breakdown) == 0 {
		t.Fatalf("breakdown = %v", result["breakdown"])
	}
	for _, b := range breakdown {
		cats := b.(map[string]any)["categories"].([]any)
		for i := 1; i < len(cats); i++ {
			prev, cur := cats[i-1].(map[string]any), cats[i].(map[string]any)
			if prev["percent"].(float64) > cur["percent"].(float64) {
				t.Errorf("categories not sorted by percent ascending: %v", cats)
			}
		}
	}
	if again := a.submit(t, id); again.Code != http.StatusConflict {
		t.Errorf("second submit: status %d, want 409", again.Code)
	} else if _, details := errorOf(t, again); details[0]["rule"] != "attempt_closed" {
		t.Errorf("second submit rule = %v, want attempt_closed", details[0]["rule"])
	}
}

func TestSubmitOfASimulationReportsPassedAsABoolean(t *testing.T) { // FR-10
	a := newAttemptsAPI(t)
	id := a.mustStart(t, simulationConfig, "")["id"].(string)

	result := a.mustSubmit(t, id)

	if passed, ok := result["passed"].(bool); !ok || passed || result["scorePercent"] != float64(0) || result["total"] != float64(65) {
		t.Errorf("result = %v, want passed=false at 0%% of 65", result)
	}
}

func TestGetOfAClosedAttemptCarriesTheSameResultTheSubmitReturned(t *testing.T) { // decision 1
	a := newAttemptsAPI(t)
	start := a.mustStart(t, untimedEnd, "")
	id := start["id"].(string)
	a.mustAnswer(t, id, 0, false, keyOf(t, itemsOf(t, start)[0]))
	if _, has := a.mustGet(t, id)["result"]; has {
		t.Error("an open attempt has a result")
	}
	a.at(5 * time.Minute)
	submitted := a.mustSubmit(t, id)

	got := a.mustGet(t, id)

	if got["status"] != "submitted" || !reflect.DeepEqual(got["result"], submitted) {
		t.Errorf("GET result = %v, want the submit's %v", got["result"], submitted)
	}
}

// ── review (S06b-6) ─────────────────────────────────────────────────────────

func TestReviewOfAnOpenAttemptIsAttemptInProgress(t *testing.T) {
	a := newAttemptsAPI(t)
	id := a.mustStart(t, untimedEnd, "")["id"].(string)

	rec := a.review(t, id)

	code, details := errorOf(t, rec)
	if rec.Code != http.StatusConflict || code != "conflict" || details[0]["rule"] != "attempt_in_progress" || details[0]["field"] != "attemptId" ||
		!strings.Contains(fmt.Sprint(details[0]["message"]), id) {
		t.Errorf("status %d, %s %v; want 409 attempt_in_progress naming %s", rec.Code, code, details, id)
	}
}

func TestReviewListsEveryItemWithTheKeyAndLinksTheNoteUntilItIsDeleted(t *testing.T) { // S06b-6
	a := newAttemptsAPI(t)
	const noteID = "0abcdef0-0000-7000-8000-0000000b0001"
	a.notes.Add(a.examID, "03 · Route 53", noteID)
	start := a.mustStart(t, originSetConfig, "")
	id := start["id"].(string)
	items := itemsOf(t, start)
	a.mustAnswer(t, id, 0, true, wrongOf(t, items[0]))
	a.mustAnswer(t, id, 1, false, keyOf(t, items[1]))
	a.mustSubmit(t, id)

	rec := a.review(t, id)

	var body struct {
		Data []map[string]any `json:"data"`
		Meta map[string]any   `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	if len(body.Data) != 3 || body.Meta["totalItems"] != float64(3) {
		t.Fatalf("review = %d items, meta %v; want 3", len(body.Data), body.Meta)
	}
	first := body.Data[0]
	if first["noteId"] != noteID || first["correct"] != false || first["flagged"] != true || first["position"] != float64(0) ||
		!reflect.DeepEqual(first["correctLabels"], []any{keyOf(t, items[0])}) || !reflect.DeepEqual(first["selected"], []any{wrongOf(t, items[0])}) ||
		first["explanation"] != "explanation of ts3-a" || len(first["rationales"].(map[string]any)) != 4 {
		t.Errorf("first review item = %v", first)
	}
	if body.Data[1]["correct"] != true {
		t.Errorf("second item = %v, want correct", body.Data[1])
	}
	for _, it := range body.Data {
		if it["noteId"] != noteID { // the three Timed Set 3 questions share the noteRef "03 · Route 53 (diap. 12)"
			t.Errorf("item %v noteId = %v, want %s", it["position"], it["noteId"], noteID)
		}
	}
	for _, it := range body.Data {
		for _, o := range it["options"].([]any) {
			if _, has := o.(map[string]any)["correct"]; has {
				t.Errorf("a review option exposes a correct flag: %v", o)
			}
		}
	}

	a.notes.Delete(a.examID, "03 · Route 53")
	rec = a.review(t, id)
	var after struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &after); err != nil || len(after.Data) == 0 {
		t.Fatalf("review after the delete: %v, body %s", err, rec.Body)
	}
	for _, it := range after.Data {
		if _, has := it["noteId"]; has {
			t.Errorf("item %v noteId = %v after the note was deleted, want it absent", it["position"], it["noteId"])
		}
	}
}

func TestReviewShowsNullExplanationAndEmptyRationalesWhenTheQuestionHasNone(t *testing.T) {
	a := newAttemptsAPI(t)
	bank := a.bank.Bank(a.examID)
	for i := range bank.Questions {
		bank.Questions[i].Explanation = ""
		for j := range bank.Questions[i].Options {
			bank.Questions[i].Options[j].Rationale = ""
		}
	}
	a.bank.Set(a.examID, bank)
	id := a.mustStart(t, untimedEnd, "")["id"].(string)
	a.mustSubmit(t, id)

	rec := a.review(t, id)

	var body struct {
		Data []map[string]any `json:"data"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if len(body.Data) == 0 {
		t.Fatalf("review status %d, body %s", rec.Code, rec.Body)
	}
	first := body.Data[0]
	explanation, has := first["explanation"]
	if !has || explanation != nil || !reflect.DeepEqual(first["rationales"], map[string]any{}) {
		t.Errorf("explanation %v (present %v), rationales %v; want null and {}", explanation, has, first["rationales"])
	}
}

// ── history (S06b-7) ────────────────────────────────────────────────────────

func TestHistoryHasThreeAttemptsNewestFirstAndATrendOfTwoSimulations(t *testing.T) { // S06b-7, D-11
	a := newAttemptsAPI(t)
	run := func(config string, answered int) string {
		start := a.mustStart(t, config, "")
		id := start["id"].(string)
		for i, it := range itemsOf(t, start)[:answered] {
			a.mustAnswer(t, id, i, false, keyOf(t, it))
		}
		a.clock.Advance(40 * time.Minute)
		a.mustSubmit(t, id)
		a.clock.Advance(24 * time.Hour)
		return id
	}
	sim1 := run(simulationConfig, 0)
	practice := run(untimedEnd, 4)
	sim2 := run(simulationConfig, 65)

	rec := a.history(t)

	var body struct {
		Data []map[string]any `json:"data"`
		Meta struct {
			TotalItems float64          `json:"totalItems"`
			Trend      []map[string]any `json:"trend"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rec.Code, rec.Body)
	}
	var ids []string
	for _, row := range body.Data {
		ids = append(ids, row["id"].(string))
	}
	if !slices.Equal(ids, []string{sim2, practice, sim1}) || body.Meta.TotalItems != 3 {
		t.Errorf("order %v, totalItems %v; want newest first %v", ids, body.Meta.TotalItems, []string{sim2, practice, sim1})
	}
	if len(body.Meta.Trend) != 2 || body.Meta.Trend[0]["scorePercent"] != float64(0) || body.Meta.Trend[1]["scorePercent"] != float64(100) ||
		body.Meta.Trend[0]["at"].(string) >= body.Meta.Trend[1]["at"].(string) {
		t.Errorf("trend = %v, want the 2 simulations (0 then 100), oldest first", body.Meta.Trend)
	}
	if len(body.Data) != 3 {
		t.Fatalf("history = %d rows, want 3", len(body.Data))
	}
	latest, middle := body.Data[0], body.Data[1]
	if latest["selectionKind"] != "simulation" || latest["timingKind"] != "timed" || latest["feedback"] != "end" || latest["status"] != "submitted" ||
		latest["scorePercent"] != float64(100) || latest["passed"] != true || latest["durationSeconds"] != float64(2400) {
		t.Errorf("latest simulation row = %v", latest)
	}
	if middle["selectionKind"] != "random" || middle["timingKind"] != "untimed" || middle["scorePercent"] != float64(100) {
		t.Errorf("practice row = %v", middle)
	}
	if _, has := middle["passed"]; has {
		t.Errorf("a practice row has passed = %v, want it absent", middle["passed"])
	}
}

func TestHistoryOfAnExamWithoutAttemptsIsEmptyListsAndAnOpenAttemptHasNoOutcome(t *testing.T) {
	a := newAttemptsAPI(t)
	rec := a.history(t)
	if got := rec.Body.String(); !strings.Contains(got, `"data":[]`) || !strings.Contains(got, `"trend":[]`) || !strings.Contains(got, `"totalItems":0`) {
		t.Errorf("empty history body = %s", got)
	}

	id := a.mustStart(t, untimedEnd, "")["id"].(string)
	var body struct {
		Data []map[string]any `json:"data"`
	}
	_ = json.Unmarshal(a.history(t).Body.Bytes(), &body)
	if len(body.Data) != 1 {
		t.Fatalf("history = %v, want the open attempt", body.Data)
	}
	row := body.Data[0]
	for _, absent := range []string{"durationSeconds", "scorePercent", "passed"} {
		if _, has := row[absent]; has {
			t.Errorf("open row %v has %s", row["id"], absent)
		}
	}
	if row["id"] != id || row["status"] != "in_progress" {
		t.Errorf("row = %v", row)
	}
}

// ── decision 3: start finalizes an expired open attempt ────────────────────

func TestStartFinalizesAnOpenAttemptPastItsGraceInsteadOfAnswering409(t *testing.T) { // decision 3
	a := newAttemptsAPI(t)
	old := a.mustStart(t, timedEnd, "")
	oldID := old["id"].(string)
	a.mustAnswer(t, oldID, 0, false, keyOf(t, itemsOf(t, old)[0]))

	a.at(10*time.Minute + 3*time.Second)
	if rec := a.start(t, untimedEnd, ""); rec.Code != http.StatusConflict {
		t.Fatalf("inside the grace: status %d, want 409 attempt_in_progress", rec.Code)
	}
	a.at(10*time.Minute + 6*time.Second)
	rec := a.start(t, untimedEnd, "")

	if rec.Code != http.StatusCreated {
		t.Fatalf("past the grace: status %d, body %s", rec.Code, rec.Body)
	}
	if d := a.mustGet(t, oldID); d["status"] != "expired" || d["result"].(map[string]any)["scorePercent"] != float64(25) {
		t.Errorf("old attempt = %v, want expired and scored", d)
	}
}
