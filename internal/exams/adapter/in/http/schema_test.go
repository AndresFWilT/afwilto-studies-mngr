package http

import (
	"encoding/json"
	"io/fs"
	"path"
	"strings"
	"sync"
	"testing"

	"github.com/afwilto/studies-mngr/api"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

var (
	readerOnce sync.Once
	reader     *QuestionFileReader
	readerErr  error
)

// sharedReader compiles the schema once for the whole test binary, as the
// server does at startup.
func sharedReader(t *testing.T) *QuestionFileReader {
	t.Helper()
	readerOnce.Do(func() { reader, readerErr = NewQuestionFileReader() })
	if readerErr != nil {
		t.Fatalf("compiling the embedded schema: %v", readerErr)
	}
	return reader
}

func example(t *testing.T, name string) []byte {
	t.Helper()
	data, err := api.FS.ReadFile(path.Join("examples", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func exampleNames(t *testing.T) []string {
	t.Helper()
	entries, err := fs.ReadDir(api.FS, "examples")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}
	if len(names) < 2 {
		t.Fatalf("examples = %v, want at least aws-text.json and moto-images.json", names)
	}
	return names
}

func TestEveryExamplePassesStages1And2(t *testing.T) { // S03-1
	r := sharedReader(t)
	for _, name := range exampleNames(t) {
		file, errs := r.Read(example(t, name))
		if len(errs) != 0 || file == nil {
			t.Errorf("%s: file = %v, errors = %+v; want a parsed file and no errors", name, file != nil, errs)
		}
	}
}

// moto-images.json is excluded from stage 3 and 4: its media is checked in
// slice 05 (S03-1), and in this slice any media: reference is media_missing.
func TestEveryTextExamplePassesStage3OnAnEmptyExam(t *testing.T) { // S03-1
	r := sharedReader(t)
	for _, name := range exampleNames(t) {
		if name == "moto-images.json" {
			continue
		}
		file, errs := r.Read(example(t, name))
		if len(errs) != 0 || file == nil {
			t.Fatalf("%s: file = %v, errors = %+v", name, file != nil, errs)
		}
		ids := 0
		plan := domain.PlanImport(domain.PlanInput{
			ExamID: "exam", File: *file, Now: testNow,
			NewID: func() string { ids++; return "id-" + string(rune('a'+ids)) },
		})
		if len(plan.Report.Errors) != 0 {
			t.Errorf("%s: stage 3 errors %+v", name, plan.Report.Errors)
		}
	}
}

func TestTheMediaExampleStopsAtStage4InThisSlice(t *testing.T) { // S03-1 exclusion, BR-Q4 in slice 03
	file, errs := sharedReader(t).Read(example(t, "moto-images.json"))
	if len(errs) != 0 || file == nil {
		t.Fatalf("file = %v, errors = %+v", file != nil, errs)
	}
	ids := 0
	plan := domain.PlanImport(domain.PlanInput{
		ExamID: "exam", File: *file, Now: testNow,
		NewID: func() string { ids++; return "id-" + string(rune('a'+ids)) },
	})
	if len(plan.Report.Errors) == 0 {
		t.Fatal("moto-images.json must be rejected for its media: references")
	}
	for _, e := range plan.Report.Errors {
		if e.Rule != domain.RuleMediaMissing {
			t.Errorf("error %+v, want only media_missing", e)
		}
	}
}

func TestReadKeepsTheFileSourceAndTheCategoryOrder(t *testing.T) { // FR-2, FR-3.1 first-appearance order
	file, errs := sharedReader(t).Read(example(t, "aws-text.json"))
	if len(errs) != 0 || file == nil {
		t.Fatalf("file = %v, errors = %+v", file != nil, errs)
	}
	if file.Source == nil || file.Source.Kind != "own" || file.Source.Name != "Spec 18 example (AWS, text only)" {
		t.Errorf("file source = %+v", file.Source)
	}
	if len(file.Questions) != 3 {
		t.Fatalf("questions = %d, want 3", len(file.Questions))
	}
	q := file.Questions[0]
	if q.Key != "example-aws-0001" || q.Type != domain.QuestionSingle || q.Difficulty != "easy" || len(q.Options) != 3 {
		t.Errorf("question 0 = %+v", q)
	}
	if !q.Options[1].Correct || q.Options[1].Rationale == "" || q.Options[0].Correct {
		t.Errorf("options = %+v", q.Options)
	}
	want := []domain.ClassifiedNames{
		{Classification: "Dominio", Names: []string{"Arquitecturas seguras"}},
		{Classification: "Servicio", Names: []string{"IAM", "Amazon S3"}},
	}
	if len(q.Categories) != 2 || q.Categories[0].Classification != want[0].Classification || q.Categories[1].Classification != want[1].Classification ||
		strings.Join(q.Categories[1].Names, "|") != "IAM|Amazon S3" {
		t.Errorf("categories = %+v, want %+v", q.Categories, want)
	}
	if q.Origin == nil || q.Origin.Set != "Spec examples" || q.Origin.Number != 1 {
		t.Errorf("origin = %+v", q.Origin)
	}
	if file.Questions[2].Origin == nil || file.Questions[2].Origin.NoteRef != "Route 53 (diap. 1)" {
		t.Errorf("noteRef origin = %+v", file.Questions[2].Origin)
	}
}

func TestReadKeepsTheJSONOrderOfClassificationsEvenWhenTheyAreNotAlphabetical(t *testing.T) { // first appearance beats map order
	doc := `{"format":"afwilto-questions","formatVersion":1,"questions":[{"key":"q1","type":"single","stem":"s",
	  "options":[{"text":"a","correct":true},{"text":"b","correct":false}],
	  "categories":{"Zeta":["z"],"Alfa":["a"],"Mu":["m"]}}]}`
	// three classifications in one question is a schema error (max 2), so use two
	doc = strings.Replace(doc, `,"Mu":["m"]`, "", 1)

	file, errs := sharedReader(t).Read([]byte(doc))
	if len(errs) != 0 || file == nil {
		t.Fatalf("file = %v, errors = %+v", file != nil, errs)
	}
	got := file.Questions[0].Categories
	if len(got) != 2 || got[0].Classification != "Zeta" || got[1].Classification != "Alfa" {
		t.Errorf("categories = %+v, want Zeta then Alfa", got)
	}
}

func mutate(t *testing.T, fn func(doc map[string]any)) []byte {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(example(t, "aws-text.json"), &doc); err != nil {
		t.Fatal(err)
	}
	fn(doc)
	out, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func question(doc map[string]any, i int) map[string]any {
	return doc["questions"].([]any)[i].(map[string]any)
}

func options(doc map[string]any, i int) []any { return question(doc, i)["options"].([]any) }

// S03-1: the negative mutations of the spec-draft check; each is rejected at
// stage 2 with rule `schema` and a path in the report's style.
func TestSchemaMutationsAreRejectedAtStage2WithTheirJSONPath(t *testing.T) {
	cases := []struct {
		name     string
		wantPath string
		fn       func(doc map[string]any)
	}{
		{"wrong format", "format", func(d map[string]any) { d["format"] = "x" }},
		{"wrong formatVersion", "formatVersion", func(d map[string]any) { d["formatVersion"] = 2 }},
		{"no questions", "questions", func(d map[string]any) { d["questions"] = []any{} }},
		{"bad key", "questions[0].key", func(d map[string]any) { question(d, 0)["key"] = "Bad Key" }},
		{"bad type", "questions[0].type", func(d map[string]any) { question(d, 0)["type"] = "bool" }},
		{"one option", "questions[0].options", func(d map[string]any) { question(d, 0)["options"] = options(d, 0)[:1] }},
		{"single with two correct", "questions[0].options", func(d map[string]any) {
			for _, o := range options(d, 0)[:2] {
				o.(map[string]any)["correct"] = true
			}
		}},
		{"single with no correct", "questions[0].options", func(d map[string]any) {
			for _, o := range options(d, 0) {
				o.(map[string]any)["correct"] = false
			}
		}},
		{"multi with one correct", "questions[1].options", func(d map[string]any) {
			for i, o := range options(d, 1) {
				o.(map[string]any)["correct"] = i == 0
			}
		}},
		{"image without alt", "questions[0].options[0]", func(d map[string]any) {
			options(d, 0)[0].(map[string]any)["image"] = "media:x.png"
		}},
		{"unknown property", "questions[0]", func(d map[string]any) { question(d, 0)["foo"] = 1 }},
		{"three classifications", "questions[0].categories", func(d map[string]any) {
			question(d, 0)["categories"] = map[string]any{"a": []any{"x"}, "b": []any{"y"}, "c": []any{"z"}}
		}},
		{"option with neither text nor image", "questions[0].options[1]", func(d map[string]any) {
			options(d, 0)[1] = map[string]any{"correct": true}
			options(d, 0)[0].(map[string]any)["correct"] = false
		}},
		{"unknown difficulty", "questions[0].difficulty", func(d map[string]any) { question(d, 0)["difficulty"] = "extreme" }},
		{"empty category list", "questions[0].categories.Dominio", func(d map[string]any) {
			question(d, 0)["categories"] = map[string]any{"Dominio": []any{}}
		}},
		{"classification name with spaces", `questions[0].categories["Mi clase"]`, func(d map[string]any) {
			question(d, 0)["categories"] = map[string]any{"Mi clase": []any{}}
		}},
		{"repeated category in one list", "questions[0].categories.Dominio", func(d map[string]any) {
			question(d, 0)["categories"] = map[string]any{"Dominio": []any{"A", "A"}}
		}},
		{"missing stem", "questions[0]", func(d map[string]any) { delete(question(d, 0), "stem") }},
	}
	r := sharedReader(t)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			file, errs := r.Read(mutate(t, tc.fn))

			if file != nil || len(errs) == 0 {
				t.Fatalf("file = %v, errors = %+v; want no file and at least one error", file != nil, errs)
			}
			found := false
			for _, e := range errs {
				if e.Rule != domain.RuleSchema || e.Message == "" {
					t.Errorf("error %+v: want rule %q and a message", e, domain.RuleSchema)
				}
				if e.Path == tc.wantPath {
					found = true
				}
			}
			if !found {
				t.Errorf("errors = %+v, want one at path %q", errs, tc.wantPath)
			}
		})
	}
}

func TestStage2CollectsTheErrorsOfEveryQuestion(t *testing.T) { // slice 03 §1.3: each stage collects all its errors
	_, errs := sharedReader(t).Read(mutate(t, func(d map[string]any) {
		question(d, 0)["key"] = "Bad Key"
		question(d, 2)["type"] = "bool"
		d["formatVersion"] = 3
	}))

	paths := map[string]bool{}
	for _, e := range errs {
		paths[e.Path] = true
	}
	for _, want := range []string{"formatVersion", "questions[0].key", "questions[2].type"} {
		if !paths[want] {
			t.Errorf("paths = %v, want %q among them", paths, want)
		}
	}
}

func TestAnOptionWithoutTextOrImageIsOneErrorNotOnePerBranch(t *testing.T) { // readable report
	_, errs := sharedReader(t).Read(mutate(t, func(d map[string]any) {
		options(d, 0)[1] = map[string]any{"correct": true}
		options(d, 0)[0].(map[string]any)["correct"] = false
	}))

	n := 0
	for _, e := range errs {
		if e.Path == "questions[0].options[1]" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("errors at questions[0].options[1] = %d (all: %+v), want exactly 1", n, errs)
	}
}

func TestStage1RejectsWhatIsNotOneJSONDocument(t *testing.T) { // slice 03 §1.3 stage 1: `$`, rule json
	cases := map[string]string{
		"truncated":            `{"format": "afwilto-questions", `,
		"not json":             `{hello}`,
		"trailing data":        `{"format":"afwilto-questions"} {}`,
		"trailing garbage":     `{"format":"afwilto-questions"} xyz`,
		"empty":                ``,
		"only whitespace":      "  \n ",
		"a lone array":         `[]`,
		"a bare string":        `"hello"`,
		"control character":    "{\"format\":\"a\x01\"}",
		"invalid UTF-8 inside": "{\"format\":\"a\xff\"}",
	}
	r := sharedReader(t)
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			file, errs := r.Read([]byte(doc))

			if file != nil || len(errs) != 1 || errs[0].Path != "$" || errs[0].Rule != domain.RuleJSON || errs[0].Message == "" {
				t.Errorf("file = %v, errors = %+v; want one error at $ with rule json", file != nil, errs)
			}
		})
	}
}

func TestAValidDocumentWithAByteOrderMarkIsAccepted(t *testing.T) { // editors on Windows add one
	doc := append([]byte("\xef\xbb\xbf"), example(t, "aws-text.json")...)

	file, errs := sharedReader(t).Read(doc)

	if len(errs) != 0 || file == nil {
		t.Errorf("errors = %+v", errs)
	}
}

func TestTheEmbeddedSchemaIsIdenticalToTheCopyInTheSpecFolder(t *testing.T) { // slice 03 §1.1: the backend copy is canonical, byte for byte
	// The spec folder lives in the frontend repo; when the lab is checked out
	// without it the test has nothing to compare and says so.
	spec, err := readSpecFile("questions.schema.json")
	if err != nil {
		t.Skipf("spec folder not available: %v", err)
	}
	embedded, err := api.FS.ReadFile(api.SchemaPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(spec) != string(embedded) {
		t.Error("api/questions.schema.json differs from the frontend spec copy")
	}
}
