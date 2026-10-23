package http

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"time"
)

// Phase-1 review H2: the schema stage cannot be exhausted by a file full of
// empty questions.

func emptyQuestions(n int) []byte {
	var b bytes.Buffer
	b.WriteString(`{"format":"afwilto-questions","formatVersion":1,"questions":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{}`)
	}
	b.WriteString(`]}`)
	return b.Bytes()
}

func TestMoreThanTwoThousandQuestionsIsOneMaxItemsErrorAndTheSchemaIsSkipped(t *testing.T) { // H2
	_, errs := sharedReader(t).Read(emptyQuestions(5000))

	if len(errs) != 1 {
		t.Fatalf("errors = %d, want exactly 1 (not one per missing field of 5000 questions): first %+v", len(errs), errs[0])
	}
	got := errs[0]
	if got.Path != "questions" || got.Rule != "schema" || !strings.Contains(got.Message, "2000") || !strings.Contains(got.Message, "5000") {
		t.Errorf("error = %+v, want path questions, rule schema, a message naming the 2000 limit and the 5000 found", got)
	}
}

func TestExactlyTwoThousandQuestionsStillRunTheSchema(t *testing.T) { // H2 boundary: the cap is > 2000
	_, errs := sharedReader(t).Read(emptyQuestions(2000))

	if len(errs) < 2000 {
		t.Fatalf("errors = %d, want the per-question schema errors of 2000 empty questions", len(errs))
	}
	if errs[0].Path != "questions[0]" {
		t.Errorf("first error = %+v, want a questions[0] error", errs[0])
	}
}

func TestANineMegabyteFileOfEmptyQuestionsGetsAShortReportQuickly(t *testing.T) { // H2: no 3M-error list
	e := newImportAPIEnv(t, importCap)
	var body bytes.Buffer
	body.WriteString(`{"format":"afwilto-questions","formatVersion":1,"questions":[`)
	body.WriteString(strings.Repeat(`{},`, 3_000_000))
	body.WriteString(`{}]}`)
	if body.Len() < 9_000_000 || body.Len() > 10<<20 {
		t.Fatalf("fixture is %d bytes, want about 9 MB under the 10 MiB limit", body.Len())
	}

	start := time.Now()
	rec := e.post(t, importPath(e.examID, "dryRun=true"), "big.json", body.Bytes())
	elapsed := time.Since(start)

	r := report(t, rec, http.StatusUnprocessableEntity)
	if errs := reportErrors(r); len(errs) != 1 || errs[0] != "questions|schema" {
		t.Errorf("%d errors, first %q; want the single questions|schema", len(errs), errs[0])
	}
	if rec.Body.Len() > 4096 {
		t.Errorf("response is %d bytes, want a short report", rec.Body.Len())
	}
	if elapsed > 10*time.Second {
		t.Errorf("answered in %s, want a fast rejection", elapsed)
	}
}

func TestTheHTTPReportOfTwoThousandEmptyQuestionsHasTwoHundredErrorsAndTheWarning(t *testing.T) { // H2 end to end
	e := newImportAPIEnv(t, importCap)

	rec := e.post(t, importPath(e.examID, "dryRun=true"), "empty.json", emptyQuestions(2000))

	r := report(t, rec, http.StatusUnprocessableEntity)
	if n := len(reportErrors(r)); n != 200 {
		t.Fatalf("errors = %d, want 200", n)
	}
	warnings := r["warnings"].([]any)
	found := false
	for _, w := range warnings {
		if s, _ := w.(string); strings.HasSuffix(s, " more errors not shown") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings = %v, want \"N more errors not shown\"", warnings)
	}
	if c := counts(r); c[3] != 2000 {
		t.Errorf("invalid = %v, want 2000", c[3])
	}
}
