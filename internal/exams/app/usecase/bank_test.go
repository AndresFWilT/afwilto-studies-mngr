package usecase

import (
	"errors"
	"slices"
	"testing"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
)

// bankEnv is an exam with the six-question fixture of S04-1 already imported.
type bankEnv struct {
	importEnv
	list in.ListExamQuestions
	one  in.GetQuestion
	del  in.DeleteQuestion
}

func withOrigin(q domain.FileQuestion, set string, number int) domain.FileQuestion {
	q.Origin = &domain.Origin{Set: set, Number: number}
	return q
}

func withDifficulty(q domain.FileQuestion, difficulty string) domain.FileQuestion {
	q.Difficulty = difficulty
	return q
}

// bankFile: Dominio is the primary; VPC has 3 questions, Seguridad 2 and one
// question has no Dominio category at all.
//
//	vpc-1  VPC        Timed Set 1 #1  easy
//	vpc-2  VPC        Timed Set 3 #2  hard
//	vpc-3  VPC        Timed Set 3 #1  hard
//	seg-1  Seguridad  Review Set #5   easy
//	seg-2  Seguridad  (no origin)
//	orphan Servicio/IAM only
func bankFile() *domain.QuestionFile {
	return fileOf(
		withDifficulty(withOrigin(fq("vpc-1", cn("Dominio", "VPC")), "Timed Set 1", 1), "easy"),
		withDifficulty(withOrigin(fq("vpc-2", cn("Dominio", "VPC")), "Timed Set 3", 2), "hard"),
		withDifficulty(withOrigin(fq("vpc-3", cn("Dominio", "VPC")), "Timed Set 3", 1), "hard"),
		withDifficulty(withOrigin(fq("seg-1", cn("Dominio", "Seguridad")), "Review Set", 5), "easy"),
		fq("seg-2", cn("Dominio", "Seguridad")),
		fq("orphan", cn("Servicio", "IAM")),
	)
}

func newBankEnv(t *testing.T) bankEnv {
	t.Helper()
	e := newImportEnv(t)
	r := e.run(t, false, "Dominio", bankFile())
	if !r.Applied {
		t.Fatalf("seeding the bank: %+v", r.Errors)
	}
	return bankEnv{
		importEnv: e,
		list:      NewListExamQuestionsService(e.bank),
		one:       NewGetQuestionService(e.bank),
		del:       NewDeleteQuestionService(e.bank, &testsupport.StepClock{N: 500}),
	}
}

func (e bankEnv) categoryID(t *testing.T, classification, category string) string {
	t.Helper()
	s, err := e.get.Execute(ctx, e.examID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range s.Classifications {
		for _, k := range c.Categories {
			if c.Name == classification && k.Name == category {
				return k.ID
			}
		}
	}
	t.Fatalf("no category %s/%s in %+v", classification, category, s.Classifications)
	return ""
}

func (e bankEnv) keys(t *testing.T, f domain.QuestionFilter) []string {
	t.Helper()
	rows, err := e.list.Execute(ctx, e.examID, f)
	if err != nil {
		t.Fatalf("list %+v: %v", f, err)
	}
	keys := []string{}
	for _, r := range rows {
		keys = append(keys, r.Key)
	}
	return keys
}

func (e bankEnv) questionID(t *testing.T, key string) string {
	t.Helper()
	rows, err := e.list.Execute(ctx, e.examID, domain.QuestionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Key == key {
			return r.ID
		}
	}
	t.Fatalf("no question %q", key)
	return ""
}

func TestListFiltersTheBank(t *testing.T) { // S04-1
	e := newBankEnv(t)
	vpc := e.categoryID(t, "Dominio", "VPC")
	cases := []struct {
		name   string
		filter domain.QuestionFilter
		want   []string
	}{
		{"category VPC returns its 3 questions", domain.QuestionFilter{CategoryID: vpc}, []string{"vpc-1", "vpc-3", "vpc-2"}},
		{"origin Timed Set 3 returns its 2 questions, ordered by number", domain.QuestionFilter{Origin: "Timed Set 3"}, []string{"vpc-3", "vpc-2"}},
		{"unclassified returns the question with no primary category", domain.QuestionFilter{Unclassified: true}, []string{"orphan"}},
		{"unclassified=false is no filter", domain.QuestionFilter{}, []string{"seg-1", "vpc-1", "vpc-3", "vpc-2", "orphan", "seg-2"}},
		{"filters combine with AND", domain.QuestionFilter{CategoryID: vpc, Origin: "Timed Set 3", Difficulty: "hard"}, []string{"vpc-3", "vpc-2"}},
		{"AND can leave nothing", domain.QuestionFilter{CategoryID: vpc, Origin: "Review Set"}, []string{}},
		{"difficulty", domain.QuestionFilter{Difficulty: "easy"}, []string{"seg-1", "vpc-1"}},
		{"q is a case-insensitive substring of the stem", domain.QuestionFilter{Text: "STEM OF SEG"}, []string{"seg-1", "seg-2"}},
		{"q matches inside the stem", domain.QuestionFilter{Text: "of vpc-2"}, []string{"vpc-2"}},
		{"q with nothing matching is empty, not an error", domain.QuestionFilter{Text: "zzz"}, []string{}},
		{"an unknown category id is empty, not an error", domain.QuestionFilter{CategoryID: "0abcdef0-0000-7000-8000-0000000dead0"}, []string{}},
		{"unclassified together with a category of the primary leaves nothing", domain.QuestionFilter{CategoryID: vpc, Unclassified: true}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := e.keys(t, tc.filter); !slices.Equal(got, tc.want) {
				t.Errorf("keys = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListSortsByOriginSetNumberThenKeyWithoutOriginLast(t *testing.T) { // slice 04 §1.1
	e := newBankEnv(t)

	got := e.keys(t, domain.QuestionFilter{})

	want := []string{"seg-1", "vpc-1", "vpc-3", "vpc-2", "orphan", "seg-2"}
	if !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestListRowsCarryTheSummaryFields(t *testing.T) {
	e := newBankEnv(t)

	rows, err := e.list.Execute(ctx, e.examID, domain.QuestionFilter{Origin: "Review Set"})

	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v, err %v", rows, err)
	}
	r := rows[0]
	if r.Key != "seg-1" || r.Type != domain.QuestionSingle || r.StemExcerpt != "Stem of seg-1" || r.Difficulty != "easy" ||
		r.Origin == nil || r.Origin.Set != "Review Set" || r.Origin.Number != 5 || r.Version != 1 || r.UpdatedAt.IsZero() ||
		!slices.Equal(r.Categories, []domain.CategoryRef{{Classification: "Dominio", Category: "Seguridad"}}) {
		t.Errorf("row = %+v", r)
	}
}

func TestListRejectsAnUnknownDifficultyAndAnUnknownExam(t *testing.T) {
	e := newBankEnv(t)

	_, err := e.list.Execute(ctx, e.examID, domain.QuestionFilter{Difficulty: "impossible"})
	wantViolation(t, err, "difficulty", "format")

	_, err = e.list.Execute(ctx, "0abcdef0-0000-7000-8000-0000000dead0", domain.QuestionFilter{})
	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown exam: err = %v, want ErrNotFound", err)
	}
}

func TestGetQuestionReturnsTheOptionsInStoredOrderWithCorrectFlags(t *testing.T) { // S04-2
	e := newBankEnv(t)

	q, err := e.one.Execute(ctx, e.questionID(t, "vpc-1"))

	if err != nil {
		t.Fatal(err)
	}
	if q.Key != "vpc-1" || len(q.Options) != 3 {
		t.Fatalf("question = %+v", q)
	}
	wantText, wantCorrect := []string{"wrong one", "right one", "wrong two"}, []bool{false, true, false}
	for i, o := range q.Options {
		if o.Text != wantText[i] || o.Correct != wantCorrect[i] {
			t.Errorf("option %d = %+v, want %q correct=%v", i, o, wantText[i], wantCorrect[i])
		}
	}
	if q.Options[1].Rationale != "why" {
		t.Errorf("rationale = %q", q.Options[1].Rationale)
	}
}

func TestGetQuestionOfAnUnknownIdIsNotFound(t *testing.T) {
	e := newBankEnv(t)

	_, err := e.one.Execute(ctx, "0abcdef0-0000-7000-8000-0000000dead0")

	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func weightsByName(t *testing.T, e bankEnv) map[string]int {
	t.Helper()
	s, err := e.get.Execute(ctx, e.examID)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]int{}
	for _, c := range s.Classifications {
		if c.IsPrimary() {
			for _, k := range c.Categories {
				out[k.Name] = k.WeightPercent
			}
		}
	}
	return out
}

func TestDeleteQuestionIsLogicalAndRecomputesTheInferredWeights(t *testing.T) { // S04-2
	e := newBankEnv(t)
	if w := weightsByName(t, e); w["VPC"] != 60 || w["Seguridad"] != 40 {
		t.Fatalf("weights before = %v, want 60/40", w)
	}
	id := e.questionID(t, "vpc-1")

	if err := e.del.Execute(ctx, id); err != nil {
		t.Fatal(err)
	}

	if _, err := e.one.Execute(ctx, id); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get after delete: err = %v, want ErrNotFound", err)
	}
	if got := e.keys(t, domain.QuestionFilter{}); slices.Contains(got, "vpc-1") || len(got) != 5 {
		t.Errorf("list after delete = %v", got)
	}
	if w := weightsByName(t, e); w["VPC"] != 50 || w["Seguridad"] != 50 {
		t.Errorf("weights after = %v, want 50/50 (2 of 4 each)", w)
	}
	s, _ := e.get.Execute(ctx, e.examID)
	if s.QuestionTotal != 5 || s.Version != 1 {
		t.Errorf("exam total %d version %d, want 5 and the version untouched", s.QuestionTotal, s.Version)
	}
}

func TestDeleteQuestionLeavesManualWeightsAlone(t *testing.T) { // BR-C3
	e := newBankEnv(t)
	manual := manualEdit(t, e, map[string]int{"VPC": 70, "Seguridad": 30})
	if _, err := e.update.Execute(ctx, e.examID, in.UpdateExamCommand{Version: 1, Structure: manual}); err != nil {
		t.Fatal(err)
	}

	if err := e.del.Execute(ctx, e.questionID(t, "vpc-1")); err != nil {
		t.Fatal(err)
	}

	if w := weightsByName(t, e); w["VPC"] != 70 || w["Seguridad"] != 30 {
		t.Errorf("weights = %v, want the manual 70/30", w)
	}
}

func TestDeleteQuestionTwiceOrUnknownIsNotFound(t *testing.T) {
	e := newBankEnv(t)
	id := e.questionID(t, "seg-2")
	if err := e.del.Execute(ctx, id); err != nil {
		t.Fatal(err)
	}

	for name, target := range map[string]string{"twice": id, "unknown": "0abcdef0-0000-7000-8000-0000000dead0"} {
		if err := e.del.Execute(ctx, target); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s: err = %v, want ErrNotFound", name, err)
		}
	}
}

func TestADeletedKeyCanBeImportedAgainAsANewQuestion(t *testing.T) { // the alive-only key rule
	e := newBankEnv(t)
	old := e.questionID(t, "seg-2")
	if err := e.del.Execute(ctx, old); err != nil {
		t.Fatal(err)
	}

	r := e.run(t, false, "", fileOf(fq("seg-2", cn("Dominio", "Seguridad"))))

	if r.Counts.Create != 1 || e.questionID(t, "seg-2") == old {
		t.Errorf("counts = %+v, want a new question for the freed key", r.Counts)
	}
}

func TestListSortsOriginSetsIgnoringCase(t *testing.T) {
	e := newImportEnv(t)
	e.run(t, false, "Dominio", fileOf(
		withOrigin(fq("upper", cn("Dominio", "A")), "Beta Set", 1),
		withOrigin(fq("lower", cn("Dominio", "A")), "alpha set", 1),
		withOrigin(fq("same-b", cn("Dominio", "A")), "Gamma", 2),
		withOrigin(fq("same-a", cn("Dominio", "A")), "gamma", 2),
	))
	rows, err := NewListExamQuestionsService(e.bank).Execute(ctx, e.examID, domain.QuestionFilter{})
	if err != nil {
		t.Fatal(err)
	}

	var keys []string
	for _, r := range rows {
		keys = append(keys, r.Key)
	}
	want := []string{"lower", "upper", "same-b", "same-a"} // alpha, Beta, then Gamma before gamma (exact order breaks the tie)
	if !slices.Equal(keys, want) {
		t.Errorf("order = %v, want %v", keys, want)
	}
}
