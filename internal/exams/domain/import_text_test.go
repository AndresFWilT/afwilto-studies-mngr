package domain

import "testing"

// Phase-1 review H3: Postgres text cannot hold U+0000, so stage 3 reports any
// string of the file that contains one, at that string's path.

const nul = "bad\x00text"

func TestPlanImportReportsANulByteAtThePathOfTheStringThatHoldsIt(t *testing.T) { // H3
	for name, tc := range map[string]struct {
		mutate func(q *FileQuestion)
		path   string
	}{
		"stem":               {func(q *FileQuestion) { q.Stem = nul }, "questions[0].stem"},
		"explanation":        {func(q *FileQuestion) { q.Explanation = nul }, "questions[0].explanation"},
		"difficulty":         {func(q *FileQuestion) { q.Difficulty = nul }, "questions[0].difficulty"},
		"option text":        {func(q *FileQuestion) { q.Options[0].Text = nul }, "questions[0].options[0].text"},
		"option rationale":   {func(q *FileQuestion) { q.Options[2].Rationale = nul }, "questions[0].options[2].rationale"},
		"option alt":         {func(q *FileQuestion) { q.Options[1].Alt = nul }, "questions[0].options[1].alt"},
		"category name":      {func(q *FileQuestion) { q.Categories = []ClassifiedNames{C("Dominio", nul)} }, "questions[0].categories"},
		"classification":     {func(q *FileQuestion) { q.Categories = []ClassifiedNames{C(nul, "A")} }, "questions[0].categories"},
		"origin set":         {func(q *FileQuestion) { q.Origin = &Origin{Set: nul} }, "questions[0].origin.set"},
		"origin noteRef":     {func(q *FileQuestion) { q.Origin = &Origin{NoteRef: nul} }, "questions[0].origin.noteRef"},
		"question source":    {func(q *FileQuestion) { q.Source = &Source{Kind: "own", Name: nul} }, "questions[0].source.name"},
		"key (also pattern)": {func(q *FileQuestion) { q.Key = "k\x00" }, "questions[0].key"},
	} {
		q := fq("q1")
		tc.mutate(&q)

		plan := planOf(t, Bank{}, file(q), "")

		found := false
		for _, e := range plan.Report.Errors {
			if e.Path == tc.path && e.Rule == RuleInvalidText {
				found = true
			}
		}
		if !found || plan.Write != nil {
			t.Errorf("%s: errors %+v (write nil: %v), want an invalid_text error at %s", name, plan.Report.Errors, plan.Write == nil, tc.path)
		}
	}
}

func TestPlanImportReportsANulByteInTheFileSource(t *testing.T) { // H3
	f := file(fq("q1"))
	f.Source = &Source{Kind: "own", Name: nul}

	plan := planOf(t, Bank{}, f, "")

	if len(plan.Report.Errors) != 1 || plan.Report.Errors[0].Path != "source.name" || plan.Report.Errors[0].Rule != RuleInvalidText {
		t.Errorf("errors = %+v, want one invalid_text at source.name", plan.Report.Errors)
	}
}

func TestPlanImportAcceptsTextWithoutNulBytes(t *testing.T) { // H3: other control characters and non-BMP text are fine
	q := fq("q1")
	q.Stem = "tab\tnewline\n emoji \U0001F600 ñ"

	plan := planOf(t, Bank{}, file(q), "")

	if len(plan.Report.Errors) != 0 || plan.Write == nil {
		t.Errorf("errors = %+v", plan.Report.Errors)
	}
}
