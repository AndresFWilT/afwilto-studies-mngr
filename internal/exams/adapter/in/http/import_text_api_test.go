package http

import (
	"net/http"
	"testing"
)

const questionWithNul = `{"format":"afwilto-questions","formatVersion":1,"questions":[
  {"key":"nul-1","type":"single","stem":"before\u0000after",
   "options":[{"text":"a","correct":true},{"text":"b","correct":false}]}]}`

func TestAStemWithANulByteIsAnInvalidTextErrorOnADryRunAndAnApplyWritesNothing(t *testing.T) { // H3
	e := newImportAPIEnv(t, importCap)

	dry := report(t, e.post(t, importPath(e.examID, "dryRun=true"), "nul.json", []byte(questionWithNul)), http.StatusUnprocessableEntity)
	if errs := reportErrors(dry); len(errs) != 1 || errs[0] != "questions[0].stem|invalid_text" {
		t.Errorf("dry run errors = %v, want questions[0].stem|invalid_text", errs)
	}

	applied := report(t, e.post(t, importPath(e.examID, ""), "nul.json", []byte(questionWithNul)), http.StatusUnprocessableEntity)
	if applied["applied"] != false {
		t.Errorf("applied = %v, want false", applied["applied"])
	}
	exam := decodeData(t, e.do(t, http.MethodGet, examPath(e.examID), ""))
	if exam["questionTotal"] != float64(0) {
		t.Errorf("questionTotal = %v after the refused apply, want 0", exam["questionTotal"])
	}
}
