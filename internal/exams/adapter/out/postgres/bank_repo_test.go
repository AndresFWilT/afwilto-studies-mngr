package postgres

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/usecase"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

type browserEnv struct {
	*bankEnv
	update in.UpdateExam
	merge  in.MergeCategories
	del    in.DeleteQuestion
	get    in.GetExam
}

func newBrowserEnv(t *testing.T) *browserEnv {
	t.Helper()
	b := newBankEnv(t)
	return &browserEnv{
		bankEnv: b,
		update:  usecase.NewUpdateExamService(b.exams, b.bank, realClock{}),
		merge:   usecase.NewMergeCategoriesService(b.exams, b.bank, realClock{}),
		del:     usecase.NewDeleteQuestionService(b.bank, realClock{}),
		get:     usecase.NewGetExamService(b.exams),
	}
}

func originOf(q domain.FileQuestion, set string, number int) domain.FileQuestion {
	q.Origin = &domain.Origin{Set: set, Number: number}
	return q
}

func hardness(q domain.FileQuestion, difficulty string) domain.FileQuestion {
	q.Difficulty = difficulty
	return q
}

// browserFile is the six-question fixture of S04-1; Dominio is the primary.
func browserFile() *domain.QuestionFile {
	return fileOf(
		hardness(originOf(fq("vpc-1", cn("Dominio", "VPC")), "Timed Set 1", 1), "easy"),
		hardness(originOf(fq("vpc-2", cn("Dominio", "VPC")), "Timed Set 3", 2), "hard"),
		hardness(originOf(fq("vpc-3", cn("Dominio", "VPC")), "Timed Set 3", 1), "hard"),
		hardness(originOf(fq("seg-1", cn("Dominio", "Seguridad")), "Review Set", 5), "easy"),
		fq("seg-2", cn("Dominio", "Seguridad")),
		fq("orphan", cn("Servicio", "IAM")),
	)
}

func (b *browserEnv) seedBrowser(t *testing.T) {
	t.Helper()
	if r := b.run(t, false, "Dominio", browserFile()); !r.Applied {
		t.Fatalf("seeding: %+v", r.Errors)
	}
}

func (b *browserEnv) categoryID(t *testing.T, classification, name string) string {
	t.Helper()
	bank, err := b.bank.LoadBank(ctx, b.exam.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range bank.Classifications {
		for _, k := range c.Categories {
			if c.Name == classification && k.Name == name {
				return k.ID
			}
		}
	}
	t.Fatalf("no category %s/%s", classification, name)
	return ""
}

func (b *browserEnv) questionID(t *testing.T, key string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `SELECT id FROM questions WHERE exam_id = $1 AND key = $2 AND deleted_at IS NULL`, b.exam.ID, key).Scan(&id); err != nil {
		t.Fatalf("question %q: %v", key, err)
	}
	return id
}

func (b *browserEnv) keys(t *testing.T, f domain.QuestionFilter) []string {
	t.Helper()
	rows, err := b.bank.ListQuestions(ctx, b.exam.ID, f)
	if err != nil {
		t.Fatalf("list %+v: %v", f, err)
	}
	out := []string{}
	for _, r := range rows {
		out = append(out, r.Key)
	}
	return out
}

func TestListQuestionsFiltersAndSortsInSQL(t *testing.T) { // S04-1
	b := newBrowserEnv(t)
	b.seedBrowser(t)
	vpc := b.categoryID(t, "Dominio", "VPC")
	cases := []struct {
		name   string
		filter domain.QuestionFilter
		want   []string
	}{
		{"no filter: origin set, number, key, without origin last", domain.QuestionFilter{}, []string{"seg-1", "vpc-1", "vpc-3", "vpc-2", "orphan", "seg-2"}},
		{"category VPC", domain.QuestionFilter{CategoryID: vpc}, []string{"vpc-1", "vpc-3", "vpc-2"}},
		{"origin Timed Set 3, by number", domain.QuestionFilter{Origin: "Timed Set 3"}, []string{"vpc-3", "vpc-2"}},
		{"unclassified", domain.QuestionFilter{Unclassified: true}, []string{"orphan"}},
		{"AND of category, origin and difficulty", domain.QuestionFilter{CategoryID: vpc, Origin: "Timed Set 3", Difficulty: "hard"}, []string{"vpc-3", "vpc-2"}},
		{"AND leaving nothing", domain.QuestionFilter{CategoryID: vpc, Origin: "Review Set"}, []string{}},
		{"difficulty", domain.QuestionFilter{Difficulty: "easy"}, []string{"seg-1", "vpc-1"}},
		{"q ignores case", domain.QuestionFilter{Text: "STEM OF SEG"}, []string{"seg-1", "seg-2"}},
		{"an unknown category id", domain.QuestionFilter{CategoryID: uuid.NewString()}, []string{}},
		{"a category and unclassified together", domain.QuestionFilter{CategoryID: vpc, Unclassified: true}, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := b.keys(t, tc.filter); !slices.Equal(got, tc.want) {
				t.Errorf("keys = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestListQuestionsSortsOriginSetsIgnoringCase(t *testing.T) {
	b := newBrowserEnv(t)
	b.apply(t, fileOf(
		originOf(fq("upper", cn("Dominio", "A")), "Beta Set", 1),
		originOf(fq("lower", cn("Dominio", "A")), "alpha set", 1),
		originOf(fq("same-b", cn("Dominio", "A")), "Gamma", 2),
		originOf(fq("same-a", cn("Dominio", "A")), "gamma", 2),
	))

	if got, want := b.keys(t, domain.QuestionFilter{}), []string{"lower", "upper", "same-b", "same-a"}; !slices.Equal(got, want) {
		t.Errorf("order = %v, want %v", got, want)
	}
}

func TestListQuestionsTreatsLikeWildcardsInQAsPlainCharacters(t *testing.T) {
	b := newBrowserEnv(t)
	odd := fq("odd", cn("Dominio", "A"))
	odd.Stem = "Discount of 100% on my_bucket"
	plain := fq("plain", cn("Dominio", "A"))
	plain.Stem = "Discount of 1000 on myXbucket"
	b.apply(t, fileOf(odd, plain))

	for text, want := range map[string][]string{"100%": {"odd"}, "my_bucket": {"odd"}, "%": {"odd"}, `\`: {}} {
		if got := b.keys(t, domain.QuestionFilter{Text: text}); !slices.Equal(got, want) {
			t.Errorf("q=%q: keys = %v, want %v", text, got, want)
		}
	}
}

func TestListQuestionsRowsCarryTheSummaryFields(t *testing.T) {
	b := newBrowserEnv(t)
	q := originOf(fq("rich", cn("Dominio", "Seguridad"), cn("Servicio", "IAM")), "Review Set", 9)
	q.Stem = "# Pregunta\n\nUsa **IAM** en `aws`"
	q.Difficulty = "medium"
	b.apply(t, fileOf(q))

	rows, err := b.bank.ListQuestions(ctx, b.exam.ID, domain.QuestionFilter{})

	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v, %v", rows, err)
	}
	r := rows[0]
	if r.Key != "rich" || r.Type != domain.QuestionSingle || r.StemExcerpt != "Pregunta Usa IAM en aws" || r.Difficulty != "medium" ||
		r.Origin == nil || *r.Origin != (domain.Origin{Set: "Review Set", Number: 9}) || r.Version != 1 || r.UpdatedAt.IsZero() || r.ID == "" ||
		!slices.Equal(r.Categories, []domain.CategoryRef{ref("Dominio", "Seguridad"), ref("Servicio", "IAM")}) {
		t.Errorf("row = %+v", r)
	}
}

func TestListQuestionsWithoutAPrimaryTreatsEveryQuestionAsUnclassified(t *testing.T) {
	b := newBrowserEnv(t)
	// both classifications are multi-valued in b, so no primary is resolved
	b.apply(t, fileOf(fq("a", cn("Dominio", "X"), cn("Servicio", "P")), fq("b", cn("Dominio", "X", "Y"), cn("Servicio", "P", "Q"))))

	if got := b.keys(t, domain.QuestionFilter{Unclassified: true}); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("unclassified = %v, want both", got)
	}
}

func TestListQuestionsOfAnUnknownExamIsNotFoundAndASecondExamIsNotMixedIn(t *testing.T) {
	b := newBrowserEnv(t)
	b.seedBrowser(t)
	other := b.add(t, b.subjectID, "Other")

	if _, err := b.bank.ListQuestions(ctx, uuid.NewString(), domain.QuestionFilter{}); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown exam: %v", err)
	}
	rows, err := b.bank.ListQuestions(ctx, other.ID, domain.QuestionFilter{})
	if err != nil || len(rows) != 0 {
		t.Errorf("other exam rows = %+v, %v, want an empty list", rows, err)
	}
}

func TestGetQuestionReadsTheOptionsInStoredOrderAndHidesTheDeleted(t *testing.T) { // S04-2
	b := newBrowserEnv(t)
	b.seedBrowser(t)
	id := b.questionID(t, "vpc-1")

	q, err := b.bank.GetQuestion(ctx, id)

	if err != nil {
		t.Fatal(err)
	}
	want := []domain.Option{{Text: "wrong one", Rationale: "no"}, {Text: "right one", Correct: true, Rationale: "yes"}, {Text: "wrong two"}}
	if q.ID != id || q.ExamID != b.exam.ID || q.Key != "vpc-1" || !slices.Equal(q.Options, want) || q.Origin == nil || q.Origin.Set != "Timed Set 1" {
		t.Errorf("question = %+v", q)
	}
	if err := b.del.Execute(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := b.bank.GetQuestion(ctx, id); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get after delete: %v", err)
	}
	if _, err := b.bank.GetQuestion(ctx, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get unknown: %v", err)
	}
}

func TestDeleteQuestionIsLogicalReleasesTheKeyAndRecomputesTheWeights(t *testing.T) { // S04-2
	b := newBrowserEnv(t)
	b.seedBrowser(t)
	id := b.questionID(t, "vpc-1")

	if err := b.del.Execute(ctx, id); err != nil {
		t.Fatal(err)
	}

	var deletedAt *time.Time
	var options int
	if err := pool.QueryRow(ctx, `SELECT deleted_at FROM questions WHERE id = $1`, id).Scan(&deletedAt); err != nil || deletedAt == nil {
		t.Fatalf("the row must stay, stamped deleted: %v %v", deletedAt, err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM question_options WHERE question_id = $1`, id).Scan(&options); err != nil || options != 3 {
		t.Errorf("options kept = %d, %v", options, err)
	}
	if got := b.keys(t, domain.QuestionFilter{}); slices.Contains(got, "vpc-1") || len(got) != 5 {
		t.Errorf("list = %v", got)
	}
	s, _ := b.get.Execute(ctx, b.exam.ID)
	weights := map[string]int{}
	for _, k := range s.Classifications[0].Categories {
		weights[k.Name] = k.WeightPercent
	}
	if s.QuestionTotal != 5 || weights["VPC"] != 50 || weights["Seguridad"] != 50 || s.Version != 1 {
		t.Errorf("total %d weights %v version %d, want 5, 50/50 and the version untouched", s.QuestionTotal, weights, s.Version)
	}
	if r := b.apply(t, fileOf(fq("vpc-1", cn("Dominio", "VPC")))); r.Counts.Create != 1 {
		t.Errorf("re-import of the freed key: %+v", r.Counts)
	}
	if err := b.del.Execute(ctx, id); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second delete: %v", err)
	}
}

// ── exam edit ──────────────────────────────────────────────────────────────

func (b *browserEnv) seedEdit(t *testing.T) {
	t.Helper()
	b.apply(t, fileOf(
		fq("e1", cn("Dominio", "Seguridad"), cn("Servicio", "Amazon S3")),
		fq("e2", cn("Dominio", "Seguridad"), cn("Servicio", "Storage", "Amazon S3")),
		fq("e3", cn("Dominio", "Costos"), cn("Servicio", "Storage")),
		fq("e4", cn("Dominio", "Rendimiento"), cn("Servicio", "VPC")),
		fq("e5", cn("Dominio", "Resiliencia"), cn("Servicio", "VPC", "Amazon S3")),
	))
}

func (b *browserEnv) version(t *testing.T) int {
	t.Helper()
	s, err := b.get.Execute(ctx, b.exam.ID)
	if err != nil {
		t.Fatal(err)
	}
	return s.Version
}

func (b *browserEnv) patch(t *testing.T, edit domain.ExamEdit, st domain.StructureEdit) (domain.ExamSummary, error) {
	t.Helper()
	return b.update.Execute(ctx, b.exam.ID, in.UpdateExamCommand{Version: b.version(t), Edit: edit, Structure: st})
}

func TestEditPersistsNamesPaceWeightsAndExamFieldsInOneVersion(t *testing.T) {
	b := newBrowserEnv(t)
	b.seedEdit(t)
	seg, cos, ren, res := b.categoryID(t, "Dominio", "Seguridad"), b.categoryID(t, "Dominio", "Costos"), b.categoryID(t, "Dominio", "Rendimiento"), b.categoryID(t, "Dominio", "Resiliencia")
	svc := b.classID(t, "Servicio")

	got, err := b.patch(t, domain.ExamEdit{Name: ptr("SAA-C03 v2"), PassPercent: ptr(70)}, domain.StructureEdit{
		Classifications: []domain.ClassificationRename{{ID: svc, Name: ptr("Servicios")}},
		Categories: []domain.CategoryEdit{{ID: seg, Name: ptr("Seguridad y cumplimiento"),
			Pace: domain.PaceEdit{QuestionsSet: true, Questions: ptr(10), MinutesSet: true, Minutes: ptr(20)}}},
		SetWeights: true,
		Weights:    []domain.WeightInput{{CategoryID: seg, Percent: 30}, {CategoryID: cos, Percent: 26}, {CategoryID: ren, Percent: 24}, {CategoryID: res, Percent: 20}},
	})

	if err != nil {
		t.Fatal(err)
	}
	if got.Name.String() != "SAA-C03 v2" || got.PassPercent != 70 || got.Version != 2 {
		t.Errorf("exam = %q pass %d v%d", got.Name, got.PassPercent, got.Version)
	}
	dom, services := got.Classifications[0], got.Classifications[1]
	k := dom.Categories[0]
	if dom.WeightMode != domain.WeightManual || services.Name != "Servicios" || k.Name != "Seguridad y cumplimiento" || k.Key != "seguridad-y-cumplimiento" ||
		k.PaceQuestions == nil || *k.PaceQuestions != 10 || *k.PaceMinutes != 20 {
		t.Errorf("classification/category = %+v / %+v", dom, services)
	}
	var ws []int
	for _, c := range dom.Categories {
		ws = append(ws, c.WeightPercent)
	}
	if !slices.Equal(ws, []int{30, 26, 24, 20}) {
		t.Errorf("weights = %v", ws)
	}
	q, err := b.bank.GetQuestion(ctx, b.questionID(t, "e1"))
	if err != nil || !slices.Contains(q.Categories, ref("Dominio", "Seguridad y cumplimiento")) || !slices.Contains(q.Categories, ref("Servicios", "Amazon S3")) {
		t.Errorf("the question reads the renamed names: %+v, %v", q.Categories, err)
	}

	back, err := b.patch(t, domain.ExamEdit{}, domain.StructureEdit{InferWeights: true}) // S04-4
	if err != nil {
		t.Fatal(err)
	}
	var shares []int
	for _, c := range back.Classifications[0].Categories {
		shares = append(shares, c.WeightPercent)
	}
	if back.Classifications[0].WeightMode != domain.WeightInferred || !slices.Equal(shares, []int{40, 20, 20, 20}) {
		t.Errorf("mode %s weights %v, want inferred 40/20/20/20", back.Classifications[0].WeightMode, shares)
	}
}

func (b *browserEnv) classID(t *testing.T, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `SELECT id FROM exam_classifications WHERE exam_id = $1 AND name = $2 AND deleted_at IS NULL`, b.exam.ID, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestEditCanSwapTheNamesOfTwoCategories(t *testing.T) {
	b := newBrowserEnv(t)
	b.seedEdit(t)
	s3, storage := b.categoryID(t, "Servicio", "Amazon S3"), b.categoryID(t, "Servicio", "Storage")

	got, err := b.patch(t, domain.ExamEdit{}, domain.StructureEdit{Categories: []domain.CategoryEdit{
		{ID: s3, Name: ptr("Storage")}, {ID: storage, Name: ptr("Amazon S3")}}})

	if err != nil {
		t.Fatal(err)
	}
	names := map[string]string{}
	for _, k := range got.Classifications[1].Categories {
		names[k.ID] = k.Name
	}
	if names[s3] != "Storage" || names[storage] != "Amazon S3" {
		t.Errorf("names = %v", names)
	}
}

func TestEditSwapsThePrimaryClassification(t *testing.T) { // item 4, BR-C2
	b := newBrowserEnv(t)
	b.apply(t, fileOf(
		fq("a", cn("Dominio", "X"), cn("Servicio", "P")),
		fq("b", cn("Dominio", "X"), cn("Servicio", "Q")),
		fq("c", cn("Dominio", "Y"), cn("Servicio", "P")),
	))

	got, err := b.patch(t, domain.ExamEdit{}, domain.StructureEdit{PrimaryID: b.classID(t, "Servicio")})

	if err != nil {
		t.Fatal(err)
	}
	svc, dom := got.Classifications[1], got.Classifications[0] // creation order is kept
	if dom.Name != "Dominio" || dom.Position != domain.PositionSecondary || svc.Position != domain.PositionPrimary || svc.WeightMode != domain.WeightInferred {
		t.Fatalf("classifications = %+v", got.Classifications)
	}
	if svc.Categories[0].WeightPercent != 67 || svc.Categories[1].WeightPercent != 33 || dom.Categories[0].WeightPercent != 0 {
		t.Errorf("weights P=%d Q=%d old=%d", svc.Categories[0].WeightPercent, svc.Categories[1].WeightPercent, dom.Categories[0].WeightPercent)
	}

	back, err := b.patch(t, domain.ExamEdit{}, domain.StructureEdit{PrimaryID: dom.ID}) // and back
	if err != nil || !back.Classifications[0].IsPrimary() || back.Classifications[1].IsPrimary() {
		t.Errorf("swap back: %+v, %v", back.Classifications, err)
	}
}

func TestEditRefusesToSwapToAMultiValuedClassification(t *testing.T) { // S04-8
	b := newBrowserEnv(t)
	b.seedEdit(t)

	_, err := b.patch(t, domain.ExamEdit{}, domain.StructureEdit{PrimaryID: b.classID(t, "Servicio")})

	var v *domain.ValidationError
	if !errors.As(err, &v) || v.Violations[0].Rule != domain.RulePrimarySingleValued {
		t.Fatalf("err = %v", err)
	}
	if b.version(t) != 1 {
		t.Error("version moved")
	}
}

func TestEditRollsBackEverythingWhenAWriteHitsAUniqueIndex(t *testing.T) { // atomicity at the database
	b := newBrowserEnv(t)
	b.seedEdit(t)
	seg, cos := b.categoryID(t, "Dominio", "Seguridad"), b.categoryID(t, "Dominio", "Costos")
	before := rowCounts(t)

	err := b.bank.ApplyEdit(ctx, b.exam.ID, func(exam domain.Exam, bank domain.Bank) (*domain.EditWrite, error) {
		renamed := exam
		renamed.Name = domain.RehydrateExamName("renamed in the same tx")
		renamed.Version++
		return &domain.EditWrite{
			ExamID: exam.ID, Now: time.Now(), Exam: &renamed, ExpectedVersion: exam.Version,
			// the second rename clashes with an unrenamed category: the first one must roll back too
			Categories: []domain.Category{
				{ID: seg, Key: "nuevo", Name: "Nuevo", Fold: "nuevo"},
				{ID: cos, Key: "rendimiento", Name: "rendimiento", Fold: "rendimiento"},
			},
		}, nil
	})

	wantConflict(t, err, domain.RuleDuplicateName)
	if after := rowCounts(t); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("rows %v -> %v", before, after)
	}
	var name string
	if err := pool.QueryRow(ctx, `SELECT name FROM exams WHERE id = $1`, b.exam.ID).Scan(&name); err != nil || name != "SAA-C03" {
		t.Errorf("exam name = %q, %v: the exam row must roll back", name, err)
	}
	var catName string
	if err := pool.QueryRow(ctx, `SELECT name FROM exam_categories WHERE id = $1`, seg).Scan(&catName); err != nil || catName != "Seguridad" {
		t.Errorf("category name = %q, %v", catName, err)
	}
}

func TestEditWithAnExamNameThatClashesIsADuplicateName(t *testing.T) {
	b := newBrowserEnv(t)
	b.seedEdit(t)
	b.add(t, b.subjectID, "Other exam")

	_, err := b.patch(t, domain.ExamEdit{Name: ptr("OTHER EXAM")}, domain.StructureEdit{InferWeights: true})

	wantConflict(t, err, domain.RuleDuplicateName)
}

func TestApplyEditOfAnUnknownExamIsNotFoundAndANilWriteOrPlanErrorWritesNothing(t *testing.T) {
	b := newBrowserEnv(t)
	b.seedEdit(t)
	before := rowCounts(t)
	boom := errors.New("boom")
	called := false

	err := b.bank.ApplyEdit(ctx, uuid.NewString(), func(domain.Exam, domain.Bank) (*domain.EditWrite, error) { called = true; return nil, nil })
	if !errors.Is(err, domain.ErrNotFound) || called {
		t.Errorf("unknown exam: %v, plan called %v", err, called)
	}
	if err := b.bank.ApplyEdit(ctx, b.exam.ID, func(domain.Exam, domain.Bank) (*domain.EditWrite, error) { return nil, nil }); err != nil {
		t.Errorf("nil write: %v", err)
	}
	if err := b.bank.ApplyEdit(ctx, b.exam.ID, func(domain.Exam, domain.Bank) (*domain.EditWrite, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Errorf("plan error: %v", err)
	}
	if after := rowCounts(t); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("rows changed: %v -> %v", before, after)
	}
}

func TestConcurrentEditsAtTheSameVersionLetExactlyOneWin(t *testing.T) { // optimistic concurrency under the exam lock
	b := newBrowserEnv(t)
	b.seedEdit(t)
	const n = 6
	var wg sync.WaitGroup
	results := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, results[i] = b.update.Execute(ctx, b.exam.ID, in.UpdateExamCommand{Version: 1, Edit: domain.ExamEdit{PassPercent: ptr(50 + i)}})
		}()
	}
	wg.Wait()

	wins, stale := 0, 0
	for _, err := range results {
		var c *domain.ConflictError
		switch {
		case err == nil:
			wins++
		case errors.As(err, &c) && c.Rule == domain.RuleStaleVersion:
			stale++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}
	if wins != 1 || stale != n-1 || b.version(t) != 2 {
		t.Errorf("wins %d stale %d version %d, want 1, %d and 2", wins, stale, b.version(t), n-1)
	}
}

// ── merge ──────────────────────────────────────────────────────────────────

func (b *browserEnv) aliveLinks(t *testing.T, key string) []string {
	t.Helper()
	rows, err := pool.Query(ctx, `
		SELECT c.name FROM question_categories qc
		JOIN questions q ON q.id = qc.question_id JOIN exam_categories c ON c.id = qc.category_id
		WHERE q.exam_id = $1 AND q.key = $2 AND q.deleted_at IS NULL AND qc.deleted_at IS NULL
		ORDER BY c.name`, b.exam.ID, key)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		out = append(out, n)
	}
	return out
}

func TestMergeMovesLinksKeepsOneWhenQuestionHadBothAndDeletesFromLogically(t *testing.T) { // S04-6
	b := newBrowserEnv(t)
	b.seedEdit(t)
	storage, s3 := b.categoryID(t, "Servicio", "Storage"), b.categoryID(t, "Servicio", "Amazon S3")
	var linkRows int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM question_categories`).Scan(&linkRows); err != nil {
		t.Fatal(err)
	}

	got, err := b.merge.Execute(ctx, b.exam.ID, in.MergeCategoriesCommand{From: storage, Into: s3})

	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 {
		t.Errorf("version = %d", got.Version)
	}
	for _, k := range got.Classifications[1].Categories {
		if k.Name == "Storage" {
			t.Error("Storage still shows in the exam detail")
		}
	}
	if links := b.aliveLinks(t, "e2"); !slices.Equal(links, []string{"Amazon S3", "Seguridad"}) {
		t.Errorf("e2 alive links = %v, want one link to Amazon S3", links)
	}
	if links := b.aliveLinks(t, "e3"); !slices.Equal(links, []string{"Amazon S3", "Costos"}) {
		t.Errorf("e3 alive links = %v, want its Storage link moved", links)
	}
	var deleted *time.Time
	var s3Count int
	if err := pool.QueryRow(ctx, `SELECT deleted_at FROM exam_categories WHERE id = $1`, storage).Scan(&deleted); err != nil || deleted == nil {
		t.Errorf("Storage must stay as a row, stamped deleted: %v %v", deleted, err)
	}
	for _, k := range got.Classifications[1].Categories {
		if k.ID == s3 {
			s3Count = k.QuestionCount
		}
	}
	var after int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM question_categories`).Scan(&after); err != nil || after != linkRows {
		t.Errorf("link rows %d -> %d, err %v: a merge only updates rows", linkRows, after, err)
	}
	if s3Count != 4 { // e1 e2 e3 e5
		t.Errorf("Amazon S3 question count = %d, want 4", s3Count)
	}
}

func TestMergeRecomputesOrAddsTheWeightsOfThePrimary(t *testing.T) {
	b := newBrowserEnv(t)
	b.seedEdit(t)
	seg, cos := b.categoryID(t, "Dominio", "Seguridad"), b.categoryID(t, "Dominio", "Costos")

	inferred, err := b.merge.Execute(ctx, b.exam.ID, in.MergeCategoriesCommand{From: cos, Into: seg})
	if err != nil {
		t.Fatal(err)
	}
	var ws []int
	for _, k := range inferred.Classifications[0].Categories {
		ws = append(ws, k.WeightPercent)
	}
	if !slices.Equal(ws, []int{60, 20, 20}) {
		t.Errorf("inferred weights after the merge = %v, want 60/20/20", ws)
	}

	ren, res := b.categoryID(t, "Dominio", "Rendimiento"), b.categoryID(t, "Dominio", "Resiliencia")
	if _, err := b.patch(t, domain.ExamEdit{}, domain.StructureEdit{SetWeights: true, Weights: []domain.WeightInput{
		{CategoryID: seg, Percent: 10}, {CategoryID: ren, Percent: 20}, {CategoryID: res, Percent: 70}}}); err != nil {
		t.Fatal(err)
	}
	manual, err := b.merge.Execute(ctx, b.exam.ID, in.MergeCategoriesCommand{From: seg, Into: ren})
	if err != nil {
		t.Fatal(err)
	}
	ws = nil
	for _, k := range manual.Classifications[0].Categories {
		ws = append(ws, k.WeightPercent)
	}
	if !slices.Equal(ws, []int{30, 70}) || manual.Classifications[0].WeightMode != domain.WeightManual { // Rendimiento 20 + Seguridad 10
		t.Errorf("manual weights after the merge = %v, want 30/70", ws)
	}
}

func TestMergeScopeIsEnforcedAgainstTheDatabaseState(t *testing.T) { // S04-7
	b := newBrowserEnv(t)
	b.seedEdit(t)
	seg, s3, storage := b.categoryID(t, "Dominio", "Seguridad"), b.categoryID(t, "Servicio", "Amazon S3"), b.categoryID(t, "Servicio", "Storage")
	if _, err := b.merge.Execute(ctx, b.exam.ID, in.MergeCategoriesCommand{From: storage, Into: s3}); err != nil {
		t.Fatal(err)
	}

	for name, cmd := range map[string]in.MergeCategoriesCommand{
		"different classifications": {From: seg, Into: s3},
		"a deleted category":        {From: storage, Into: s3},
		"a category of nowhere":     {From: uuid.NewString(), Into: s3},
	} {
		_, err := b.merge.Execute(ctx, b.exam.ID, cmd)
		var v *domain.ValidationError
		if !errors.As(err, &v) || v.Violations[0].Rule != domain.RuleMergeScope {
			t.Errorf("%s: %v", name, err)
		}
	}
	if b.version(t) != 2 {
		t.Errorf("version = %d", b.version(t))
	}
}
