package http

import (
	"fmt"
	"testing"
)

// Phase-1 review M2: JSON Schema counts 1.0 and 1e3 as integers, so the
// decoder must too; a value that is an integer but does not fit never turns
// into a decode error at `$`.

func fileWithOriginNumber(literal string) []byte {
	return []byte(fmt.Sprintf(`{"format":"afwilto-questions","formatVersion":1,"questions":[
  {"key":"n-1","type":"single","stem":"s","categories":{"Dominio":["A"]},
   "options":[{"text":"a","correct":true},{"text":"b","correct":false}],
   "origin":{"set":"S","number":%s}}]}`, literal))
}

func TestOriginNumberAcceptsEveryIntegerSpellingTheSchemaAccepts(t *testing.T) { // M2
	for literal, want := range map[string]int{
		"7":                7,
		"1.0":              1,
		"1e3":              1000,
		"2.5e1":            25,
		"9007199254740993": 9007199254740993, // beyond float64's exact range: must not round
	} {
		file, errs := sharedReader(t).Read(fileWithOriginNumber(literal))

		if len(errs) != 0 {
			t.Errorf("number %s: errors %+v, want none", literal, errs)
			continue
		}
		if got := file.Questions[0].Origin.Number; got != want {
			t.Errorf("number %s: decoded %d, want %d", literal, got, want)
		}
	}
}

func TestOriginNumberOutsideTheIntegerRangeIsAStage2ErrorAtItsJSONPath(t *testing.T) { // M2
	for _, literal := range []string{"1e30", "9223372036854775808", "100000000000000000000"} {
		file, errs := sharedReader(t).Read(fileWithOriginNumber(literal))

		if file != nil || len(errs) != 1 {
			t.Errorf("number %s: file %v, errors %+v; want one error and no file", literal, file != nil, errs)
			continue
		}
		if errs[0].Path != "questions[0].origin.number" || errs[0].Rule != "schema" {
			t.Errorf("number %s: error %+v, want rule schema at questions[0].origin.number (never a $ decode error)", literal, errs[0])
		}
	}
}

func TestOriginNumberFractionsAndZeroStillFailTheSchema(t *testing.T) { // M2 must not loosen stage 2
	for _, literal := range []string{"1.5", "0", "-3", `"7"`} {
		_, errs := sharedReader(t).Read(fileWithOriginNumber(literal))

		if len(errs) != 1 || errs[0].Path != "questions[0].origin.number" || errs[0].Rule != "schema" {
			t.Errorf("number %s: errors %+v, want one schema error at questions[0].origin.number", literal, errs)
		}
	}
}
