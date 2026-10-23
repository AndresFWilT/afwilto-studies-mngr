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

type bankEnv struct {
	*repoEnv
	bank     *QuestionBankRepository
	exams    *ExamRepository
	exam     domain.Exam
	tick     int
	importer in.ImportQuestions
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now().UTC() }

type uuidGen struct{}

func (uuidGen) NewID() string { return uuid.NewString() }

func newBankEnv(t *testing.T) *bankEnv {
	t.Helper()
	e := newRepoEnv(t)
	b := &bankEnv{repoEnv: e, bank: NewQuestionBankRepository(pool), exams: e.repo}
	b.exam = e.add(t, e.subjectID, "SAA-C03")
	b.importer = usecase.NewImportQuestionsService(b.bank, realClock{}, uuidGen{})
	return b
}

func ref(classification, category string) domain.CategoryRef {
	return domain.CategoryRef{Classification: classification, Category: category}
}

func cn(classification string, names ...string) domain.ClassifiedNames {
	return domain.ClassifiedNames{Classification: classification, Names: names}
}

func fq(key string, categories ...domain.ClassifiedNames) domain.FileQuestion {
	return domain.FileQuestion{
		Key: key, Type: domain.QuestionSingle, Stem: "Stem of " + key,
		Options:    []domain.Option{{Text: "wrong one", Rationale: "no"}, {Text: "right one", Correct: true, Rationale: "yes"}, {Text: "wrong two"}},
		Categories: categories,
	}
}

func fileOf(qs ...domain.FileQuestion) *domain.QuestionFile {
	return &domain.QuestionFile{Source: &domain.Source{Kind: "own", Name: "fixture"}, Questions: qs}
}

func awsFile() *domain.QuestionFile {
	q1 := fq("example-aws-0001", cn("Dominio", "Arquitecturas seguras"), cn("Servicio", "IAM", "Amazon S3"))
	q1.Explanation, q1.Difficulty, q1.Origin = "Because *roles*.", "easy", &domain.Origin{Set: "Spec examples", Number: 1}
	q3 := fq("example-aws-0003", cn("Dominio", "Arquitecturas resilientes"), cn("Servicio", "Route 53"))
	q3.Origin = &domain.Origin{NoteRef: "Route 53 (diap. 1)"}
	return fileOf(q1, fq("example-aws-0002", cn("Dominio", "Alto rendimiento"), cn("Servicio", "Amazon S3")), q3)
}

func (b *bankEnv) run(t *testing.T, dry bool, primary string, f *domain.QuestionFile) domain.ImportReport {
	t.Helper()
	r, err := b.importer.Execute(ctx, in.ImportQuestionsCommand{ExamID: b.exam.ID, DryRun: dry, Primary: primary, File: f})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return r
}

func (b *bankEnv) apply(t *testing.T, f *domain.QuestionFile) domain.ImportReport {
	t.Helper()
	r := b.run(t, false, "", f)
	if !r.Applied || len(r.Errors) != 0 {
		t.Fatalf("apply: applied %v errors %+v", r.Applied, r.Errors)
	}
	return r
}

// examTables are the tables a dry run must leave exactly as they were (S03-8).
var examTables = []string{"exams", "exam_classifications", "exam_categories", "questions", "question_options", "question_categories"}

func rowCounts(t *testing.T) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, table := range examTables {
		var n int
		if err := pool.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		out[table] = n
	}
	return out
}

// examsChecksum fingerprints every row of every exams-context table in full
// (version, updated_at and deleted_at included), so a write that keeps the row
// counts, such as a version bump or a logical delete, still changes it.
func examsChecksum(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range examTables {
		var sum string
		if err := pool.QueryRow(ctx, "SELECT coalesce(md5(string_agg(x::text, '|' ORDER BY x::text)), '') FROM "+table+" x").Scan(&sum); err != nil {
			t.Fatal(err)
		}
		out[table] = sum
	}
	return out
}

func TestLoadBankOfAnEmptyExamIsEmpty(t *testing.T) {
	b := newBankEnv(t)

	bank, err := b.bank.LoadBank(ctx, b.exam.ID)

	if err != nil || len(bank.Classifications) != 0 || len(bank.Questions) != 0 {
		t.Errorf("bank = %+v, %v", bank, err)
	}
}

func TestLoadBankAndApplyImportOfAnUnknownOrDeletedExamAreNotFound(t *testing.T) {
	b := newBankEnv(t)
	gone := b.add(t, b.subjectID, "Gone")
	gone.Delete(time.Now())
	if err := b.exams.Delete(ctx, gone); err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{"unknown": uuid.NewString(), "deleted": gone.ID} {
		if _, err := b.bank.LoadBank(ctx, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("%s LoadBank: %v", name, err)
		}
		called := false
		err := b.bank.ApplyImport(ctx, id, func(domain.Bank) (*domain.ImportWrite, error) { called = true; return nil, nil })
		if !errors.Is(err, domain.ErrNotFound) || called {
			t.Errorf("%s ApplyImport: err %v, plan called %v", name, err, called)
		}
	}
}

func TestApplyPersistsTheImportAndLoadBankReadsItBack(t *testing.T) { // AC-1 apply, FR-3, slice 03 §1.6
	b := newBankEnv(t)

	r := b.apply(t, awsFile())

	if r.Counts.Create != 3 {
		t.Fatalf("counts = %+v", r.Counts)
	}
	bank, err := b.bank.LoadBank(ctx, b.exam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(bank.Classifications) != 2 || bank.Classifications[0].Name != "Dominio" || bank.Classifications[0].Position != domain.PositionPrimary ||
		bank.Classifications[1].Name != "Servicio" || bank.Classifications[1].Position != domain.PositionSecondary ||
		bank.Classifications[0].WeightMode != domain.WeightInferred {
		t.Fatalf("classifications = %+v", bank.Classifications)
	}
	var primary []string
	var weights []int
	for _, c := range bank.Classifications[0].Categories {
		primary = append(primary, c.Name)
		weights = append(weights, c.WeightPercent)
		if c.Key != domain.Slug(c.Name) || c.QuestionCount != 1 {
			t.Errorf("category %+v: want slug key and one question", c)
		}
	}
	if !slices.Equal(primary, []string{"Arquitecturas seguras", "Alto rendimiento", "Arquitecturas resilientes"}) || !slices.Equal(weights, []int{34, 33, 33}) {
		t.Errorf("primary categories %v weights %v", primary, weights)
	}
	if len(bank.Classifications[1].Categories) != 3 { // IAM, Amazon S3, Route 53
		t.Errorf("services = %+v", bank.Classifications[1].Categories)
	}
	if bank.Classifications[1].Categories[1].QuestionCount != 2 { // Amazon S3 is on two questions
		t.Errorf("Amazon S3 question count = %d, want 2", bank.Classifications[1].Categories[1].QuestionCount)
	}

	if len(bank.Questions) != 3 {
		t.Fatalf("questions = %d", len(bank.Questions))
	}
	q := bank.Questions[0]
	if q.Key != "example-aws-0001" || q.Type != domain.QuestionSingle || q.Stem != "Stem of example-aws-0001" || q.Explanation != "Because *roles*." ||
		q.Difficulty != "easy" || q.Version != 1 || q.ExamID != b.exam.ID || q.Fingerprint != domain.StemFingerprint(q.Stem) ||
		q.Source != (domain.Source{Kind: "own", Name: "fixture"}) {
		t.Errorf("question 0 = %+v", q)
	}
	if q.Origin == nil || *q.Origin != (domain.Origin{Set: "Spec examples", Number: 1}) {
		t.Errorf("origin = %+v", q.Origin)
	}
	if bank.Questions[2].Origin == nil || *bank.Questions[2].Origin != (domain.Origin{NoteRef: "Route 53 (diap. 1)"}) || bank.Questions[1].Origin != nil {
		t.Errorf("other origins = %+v / %+v", bank.Questions[2].Origin, bank.Questions[1].Origin)
	}
	wantOptions := []domain.Option{{Text: "wrong one", Rationale: "no"}, {Text: "right one", Correct: true, Rationale: "yes"}, {Text: "wrong two"}}
	if !slices.Equal(q.Options, wantOptions) {
		t.Errorf("options = %+v, want %+v in this order", q.Options, wantOptions)
	}
	wantRefs := []domain.CategoryRef{ref("Dominio", "Arquitecturas seguras"), ref("Servicio", "IAM"), ref("Servicio", "Amazon S3")}
	if len(q.Categories) != 3 {
		t.Errorf("categories = %+v", q.Categories)
	}
	for _, want := range wantRefs {
		if !slices.Contains(q.Categories, want) {
			t.Errorf("categories = %+v, missing %+v", q.Categories, want)
		}
	}
}

func TestExamSummaryAndDetailReadTheBank(t *testing.T) { // slice 02 "filled in by slice 03"
	b := newBankEnv(t)
	b.apply(t, awsFile())

	summary, err := b.exams.GetSummary(ctx, b.exam.ID)
	list, listErr := b.exams.ListBySubject(ctx, b.subjectID)

	if err != nil || listErr != nil {
		t.Fatal(err, listErr)
	}
	if summary.QuestionTotal != 3 || len(summary.Classifications) != 2 || summary.Classifications[0].Categories[0].WeightPercent != 34 {
		t.Errorf("summary = %+v", summary)
	}
	if len(list) != 1 || list[0].QuestionTotal != 3 || list[0].Classifications != nil {
		t.Errorf("list = %+v, want the total but no classifications", list)
	}
}

func TestReimportingTheSameFileChangesNoRowAtAll(t *testing.T) { // AC-2
	b := newBankEnv(t)
	b.apply(t, awsFile())
	before := rowCounts(t)
	var updatedBefore time.Time
	if err := pool.QueryRow(ctx, `SELECT max(updated_at) FROM questions`).Scan(&updatedBefore); err != nil {
		t.Fatal(err)
	}

	r := b.apply(t, awsFile())

	if r.Counts != (domain.ImportCounts{Unchanged: 3}) || len(r.NewCategories) != 0 || len(r.DuplicateStems) != 0 {
		t.Errorf("report = %+v", r)
	}
	var updatedAfter time.Time
	if err := pool.QueryRow(ctx, `SELECT max(updated_at) FROM questions`).Scan(&updatedAfter); err != nil {
		t.Fatal(err)
	}
	if after := rowCounts(t); fmt.Sprint(after) != fmt.Sprint(before) || !updatedAfter.Equal(updatedBefore) {
		t.Errorf("rows %v -> %v, updated_at %v -> %v", before, after, updatedBefore, updatedAfter)
	}
}

func TestADryRunNeverWrites(t *testing.T) { // S03-8: a checksum of whole rows, not a row count
	b := newBankEnv(t)
	b.apply(t, fileOf(fq("seed", cn("Dominio", "Seguridad"))))
	before := examsChecksum(t)

	edited := fq("seed", cn("Dominio", "Seguridad"))
	edited.Stem = "edited"
	r := b.run(t, true, "", fileOf(edited, fq("new-one", cn("Dominio", "Costos"), cn("Servicio", "S3"))))

	if !r.DryRun || r.Applied || r.Counts != (domain.ImportCounts{Create: 1, Update: 1}) {
		t.Fatalf("report = %+v", r)
	}
	after := examsChecksum(t)
	for _, table := range examTables {
		if after[table] != before[table] {
			t.Errorf("%s changed by a dry run (a checksum of its whole rows, deleted_at, version and updated_at included, differs)", table)
		}
	}
	var stem string
	var version int
	if err := pool.QueryRow(ctx, `SELECT stem, version FROM questions WHERE key = 'seed' AND deleted_at IS NULL`).Scan(&stem, &version); err != nil {
		t.Fatal(err)
	}
	if stem != "Stem of seed" || version != 1 {
		t.Errorf("stored question changed: %q v%d", stem, version)
	}
}

func TestAFileWithAnInvalidQuestionWritesNothing(t *testing.T) { // AC-4
	b := newBankEnv(t)
	before := rowCounts(t)
	bad := fq("bad")
	bad.Options = []domain.Option{{Text: "a", Correct: true}, {Text: "b", Correct: true}, {Text: "c"}}

	r := b.run(t, false, "", fileOf(fq("one", cn("Dominio", "A")), fq("two", cn("Dominio", "A")), fq("three", cn("Dominio", "B")), bad))

	if r.Applied || len(r.Errors) != 1 || r.Errors[0].Path != "questions[3].options" || r.Errors[0].Rule != "correct_count" {
		t.Fatalf("report = applied %v errors %+v", r.Applied, r.Errors)
	}
	if after := rowCounts(t); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("rows %v -> %v: the invalid file must leave the DB unchanged", before, after)
	}
}

func TestAWriteThatFailsHalfwayLeavesNoRowBehind(t *testing.T) { // BR-I1 atomicity at the database
	b := newBankEnv(t)
	b.apply(t, fileOf(fq("taken", cn("Dominio", "A"))))
	before := rowCounts(t)
	good := domain.Question{ID: uuid.NewString(), ExamID: b.exam.ID, Key: "fresh", Type: domain.QuestionSingle, Stem: "s",
		Options: []domain.Option{{Text: "a", Correct: true}, {Text: "b"}}, Source: domain.Source{Kind: "own", Name: "x"},
		Fingerprint: domain.StemFingerprint("s"), Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	clash := good
	clash.ID, clash.Key = uuid.NewString(), "taken" // the unique (exam, key) index rejects this one, after the first insert
	catID, classID := uuid.NewString(), uuid.NewString()

	err := b.bank.ApplyImport(ctx, b.exam.ID, func(domain.Bank) (*domain.ImportWrite, error) {
		return &domain.ImportWrite{
			ExamID: b.exam.ID, Now: time.Now(),
			Classifications: []domain.Classification{{ID: classID, ExamID: b.exam.ID, Name: "Nivel", Fold: "nivel", Position: domain.PositionSecondary, WeightMode: domain.WeightInferred}},
			Categories:      []domain.Category{{ID: catID, ClassificationID: classID, Key: "alto", Name: "Alto", Fold: "alto"}},
			Creates:         []domain.QuestionWrite{{Question: good, CategoryIDs: []string{catID}}, {Question: clash, CategoryIDs: []string{catID}}},
		}, nil
	})

	if err == nil {
		t.Fatal("the clashing key must fail the write")
	}
	if after := rowCounts(t); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("rows %v -> %v: the classification, category and first question must be rolled back", before, after)
	}
}

func TestApplyImportWithANilWriteOrAPlanErrorWritesNothing(t *testing.T) {
	b := newBankEnv(t)
	before := rowCounts(t)
	boom := errors.New("boom")

	if err := b.bank.ApplyImport(ctx, b.exam.ID, func(domain.Bank) (*domain.ImportWrite, error) { return nil, nil }); err != nil {
		t.Errorf("nil write: %v", err)
	}
	if err := b.bank.ApplyImport(ctx, b.exam.ID, func(domain.Bank) (*domain.ImportWrite, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Errorf("plan error: %v", err)
	}
	if after := rowCounts(t); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("rows changed: %v -> %v", before, after)
	}
}

func TestAnUpdateKeepsTheIdBumpsTheVersionAndReplacesOptionsAndLinks(t *testing.T) { // AC-8, slice 03 §4 (options replaced by marking the old set deleted)
	b := newBankEnv(t)
	b.apply(t, awsFile())
	var id string
	var version int
	if err := pool.QueryRow(ctx, `SELECT id, version FROM questions WHERE key = 'example-aws-0002'`).Scan(&id, &version); err != nil {
		t.Fatal(err)
	}
	edited := awsFile()
	edited.Questions[1].Stem = "A rewritten stem"
	edited.Questions[1].Options = []domain.Option{{Text: "only", Correct: true}, {Text: "two"}}
	edited.Questions[1].Categories = []domain.ClassifiedNames{cn("Dominio", "Alto rendimiento"), cn("Servicio", "IAM")}

	r := b.apply(t, edited)

	if r.Counts != (domain.ImportCounts{Update: 1, Unchanged: 2}) {
		t.Fatalf("counts = %+v", r.Counts)
	}
	var gotID, stem string
	var gotVersion int
	var created, updated time.Time
	if err := pool.QueryRow(ctx, `SELECT id, stem, version, created_at, updated_at FROM questions WHERE key = 'example-aws-0002' AND deleted_at IS NULL`).
		Scan(&gotID, &stem, &gotVersion, &created, &updated); err != nil {
		t.Fatal(err)
	}
	if gotID != id || gotVersion != version+1 || stem != "A rewritten stem" || !updated.After(created) {
		t.Errorf("id %s->%s version %d->%d stem %q", id, gotID, version, gotVersion, stem)
	}
	var alive, dead int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE deleted_at IS NULL), count(*) FILTER (WHERE deleted_at IS NOT NULL) FROM question_options WHERE question_id = $1`, id).Scan(&alive, &dead); err != nil {
		t.Fatal(err)
	}
	if alive != 2 || dead != 3 {
		t.Errorf("options alive %d deleted %d, want 2 and 3", alive, dead)
	}
	bank, _ := b.bank.LoadBank(ctx, b.exam.ID)
	var q domain.Question
	for _, c := range bank.Questions {
		if c.Key == "example-aws-0002" {
			q = c
		}
	}
	if !slices.Equal(q.Options, []domain.Option{{Text: "only", Correct: true}, {Text: "two"}}) {
		t.Errorf("options = %+v", q.Options)
	}
	if len(q.Categories) != 2 || !slices.Contains(q.Categories, ref("Servicio", "IAM")) || slices.Contains(q.Categories, ref("Servicio", "Amazon S3")) {
		t.Errorf("categories = %+v, want the Amazon S3 link gone and the IAM link added", q.Categories)
	}
	if len(bank.Questions) != 3 {
		t.Errorf("questions = %d", len(bank.Questions))
	}
}

func TestAStaleExpectedVersionFailsTheWholeImport(t *testing.T) { // optimistic guard under the exam lock
	b := newBankEnv(t)
	b.apply(t, fileOf(fq("a", cn("Dominio", "A"))))
	before := rowCounts(t)

	err := b.bank.ApplyImport(ctx, b.exam.ID, func(bank domain.Bank) (*domain.ImportWrite, error) {
		q := bank.Questions[0]
		q.Version++
		q.Stem = "x"
		return &domain.ImportWrite{ExamID: b.exam.ID, Now: time.Now(),
			Updates: []domain.QuestionWrite{{Question: q, ExpectedVersion: q.Version + 5}}}, nil
	})

	wantConflict(t, err, domain.RuleStaleVersion)
	if after := rowCounts(t); fmt.Sprint(after) != fmt.Sprint(before) {
		t.Errorf("rows changed: %v -> %v", before, after)
	}
}

func TestConcurrentImportsOfTheSameFileSerialize(t *testing.T) { // the exam row lock makes plan+write one unit
	b := newBankEnv(t)
	var wg sync.WaitGroup
	reports := make([]domain.ImportReport, 4)
	errs := make([]error, 4)
	for i := range reports {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reports[i], errs[i] = b.importer.Execute(ctx, in.ImportQuestionsCommand{ExamID: b.exam.ID, File: awsFile()})
		}()
	}
	wg.Wait()

	created, unchanged := 0, 0
	for i, r := range reports {
		if errs[i] != nil || !r.Applied {
			t.Fatalf("import %d: err %v report %+v", i, errs[i], r.Errors)
		}
		created += r.Counts.Create
		unchanged += r.Counts.Unchanged
	}
	if created != 3 || unchanged != 9 {
		t.Errorf("created %d unchanged %d across the 4 imports, want 3 and 9", created, unchanged)
	}
	counts := rowCounts(t)
	if counts["questions"] != 3 || counts["exam_classifications"] != 2 || counts["exam_categories"] != 6 {
		t.Errorf("rows = %v", counts)
	}
}

func TestDeletingAnExamWithAliveQuestionsIsInUseUntilTheyAreGone(t *testing.T) { // S03-9, BR-E3
	b := newBankEnv(t)
	b.apply(t, awsFile())
	victim := b.exam
	victim.Delete(time.Now())

	wantConflict(t, b.exams.Delete(ctx, victim), domain.RuleInUse)

	if _, err := b.exams.Get(ctx, b.exam.ID); err != nil {
		t.Fatalf("the exam must still be alive: %v", err)
	}
	exec(t, `UPDATE questions SET deleted_at = now() WHERE exam_id = $1`, b.exam.ID)
	if err := b.exams.Delete(ctx, victim); err != nil {
		t.Errorf("with every question logically deleted the exam can go: %v", err)
	}
}

func TestDeletedQuestionsAreInvisibleToTheBank(t *testing.T) { // FR-0
	b := newBankEnv(t)
	b.apply(t, awsFile())
	exec(t, `UPDATE questions SET deleted_at = now() WHERE key = 'example-aws-0001'`)

	bank, _ := b.bank.LoadBank(ctx, b.exam.ID)
	r := b.run(t, true, "", awsFile())

	if len(bank.Questions) != 2 {
		t.Errorf("questions = %d, want 2", len(bank.Questions))
	}
	if r.Counts.Create != 1 || r.Counts.Unchanged != 2 {
		t.Errorf("a logically deleted key is a create again: %+v", r.Counts)
	}
}

func TestSecondApplyOfAKeyThatWasLogicallyDeletedRecreatesIt(t *testing.T) {
	b := newBankEnv(t)
	b.apply(t, awsFile())
	exec(t, `UPDATE questions SET deleted_at = now() WHERE key = 'example-aws-0001'`)

	b.apply(t, awsFile())

	var alive, total int
	if err := pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE deleted_at IS NULL), count(*) FROM questions WHERE key = 'example-aws-0001'`).Scan(&alive, &total); err != nil {
		t.Fatal(err)
	}
	if alive != 1 || total != 2 {
		t.Errorf("alive %d total %d, want 1 alive and the old row kept", alive, total)
	}
}

func TestAWriteWithATextPostgresCannotStoreIsInvalidTextAndLeavesNoRowBehind(t *testing.T) { // H3: SQLSTATE 22021 (text) and 22P05 (jsonb)
	for name, mutate := range map[string]func(q *domain.Question){
		"a NUL in a text column":    func(q *domain.Question) { q.Stem = "bad\x00stem" },
		"a NUL in the jsonb origin": func(q *domain.Question) { q.Origin = &domain.Origin{NoteRef: "bad\x00ref"} },
	} {
		b := newBankEnv(t)
		before := rowCounts(t)
		q := domain.Question{ID: uuid.NewString(), ExamID: b.exam.ID, Key: "fresh", Type: domain.QuestionSingle, Stem: "s",
			Options: []domain.Option{{Text: "a", Correct: true}, {Text: "b"}}, Source: domain.Source{Kind: "own", Name: "x"},
			Fingerprint: domain.StemFingerprint("s"), Version: 1, CreatedAt: time.Now(), UpdatedAt: time.Now()}
		mutate(&q)

		err := b.bank.ApplyImport(ctx, b.exam.ID, func(domain.Bank) (*domain.ImportWrite, error) {
			return &domain.ImportWrite{ExamID: b.exam.ID, Now: time.Now(), Creates: []domain.QuestionWrite{{Question: q}}}, nil
		})

		if !errors.Is(err, domain.ErrInvalidText) {
			t.Errorf("%s: err = %v, want domain.ErrInvalidText", name, err)
		}
		if after := rowCounts(t); fmt.Sprint(after) != fmt.Sprint(before) {
			t.Errorf("%s: rows %v -> %v, want nothing written", name, before, after)
		}
	}
}
