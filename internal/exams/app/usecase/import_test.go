package usecase

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
)

type importEnv struct {
	env
	imp    in.ImportQuestions
	examID string
}

func newImportEnv(t *testing.T) importEnv {
	t.Helper()
	e := newEnv()
	bank := e.bank
	clock := &testsupport.StepClock{}
	ids := &testsupport.SeqIDGen{N: 1000}
	exam := e.add(t, subjectA, "SAA-C03")
	// the delete service must see the bank, exactly as the SQL repository does
	return importEnv{env: e, imp: NewImportQuestionsService(bank, clock, ids), examID: exam.ID}
}

func cn(classification string, names ...string) domain.ClassifiedNames {
	return domain.ClassifiedNames{Classification: classification, Names: names}
}

// fq builds a valid single question; the second option is the correct one.
func fq(key string, categories ...domain.ClassifiedNames) domain.FileQuestion {
	return domain.FileQuestion{
		Key: key, Type: domain.QuestionSingle, Stem: "Stem of " + key,
		Options:    []domain.Option{{Text: "wrong one"}, {Text: "right one", Correct: true, Rationale: "why"}, {Text: "wrong two"}},
		Categories: categories,
	}
}

func fileOf(questions ...domain.FileQuestion) *domain.QuestionFile {
	return &domain.QuestionFile{Source: &domain.Source{Kind: "own", Name: "fixture"}, Questions: questions}
}

// awsFile mirrors api/examples/aws-text.json (AC-1).
func awsFile() *domain.QuestionFile {
	return fileOf(
		fq("example-aws-0001", cn("Dominio", "Arquitecturas seguras"), cn("Servicio", "IAM", "Amazon S3")),
		fq("example-aws-0002", cn("Dominio", "Alto rendimiento"), cn("Servicio", "Amazon S3")),
		fq("example-aws-0003", cn("Dominio", "Arquitecturas resilientes"), cn("Servicio", "Route 53")),
	)
}

func (e importEnv) run(t *testing.T, dryRun bool, primary string, file *domain.QuestionFile) domain.ImportReport {
	t.Helper()
	report, err := e.imp.Execute(ctx, in.ImportQuestionsCommand{ExamID: e.examID, DryRun: dryRun, Primary: primary, File: file})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return report
}

func (e importEnv) apply(t *testing.T, file *domain.QuestionFile) domain.ImportReport {
	t.Helper()
	r := e.run(t, false, "", file)
	if len(r.Errors) != 0 || !r.Applied {
		t.Fatalf("apply: applied %v errors %+v", r.Applied, r.Errors)
	}
	return r
}

func TestDryRunOfAnEmptyExamReportsTheInferenceAndWritesNothing(t *testing.T) { // AC-1, S03-8
	e := newImportEnv(t)
	before := e.bank.Bank(e.examID)

	r := e.run(t, true, "", awsFile())

	if !r.DryRun || r.Applied {
		t.Errorf("dryRun %v applied %v, want true/false", r.DryRun, r.Applied)
	}
	if r.Counts != (domain.ImportCounts{Create: 3}) {
		t.Errorf("counts = %+v, want 3 create", r.Counts)
	}
	if !slices.Equal(r.NewClassifications, []string{"Dominio", "Servicio"}) || len(r.NewCategories) != 6 {
		t.Errorf("new classifications %v, new categories %d", r.NewClassifications, len(r.NewCategories))
	}
	if !reflect.DeepEqual(e.bank.Bank(e.examID), before) || e.bank.Applied != 0 {
		t.Error("a dry run must not write")
	}
	summary, _ := e.get.Execute(ctx, e.examID)
	if summary.QuestionTotal != 0 || len(summary.Classifications) != 0 {
		t.Errorf("exam after a dry run = %+v", summary)
	}
}

func TestApplyThenReimportReportsEveryQuestionUnchanged(t *testing.T) { // AC-2
	e := newImportEnv(t)
	first := e.run(t, false, "", awsFile())
	if !first.Applied || first.DryRun || first.Counts.Create != 3 {
		t.Fatalf("first import: %+v", first)
	}
	afterFirst := e.bank.Bank(e.examID)

	second := e.run(t, true, "", awsFile())

	if second.Counts != (domain.ImportCounts{Unchanged: 3}) || len(second.NewCategories) != 0 || len(second.NewClassifications) != 0 || len(second.DuplicateStems) != 0 {
		t.Errorf("second dry run = counts %+v new categories %v duplicates %v", second.Counts, second.NewCategories, second.DuplicateStems)
	}
	third := e.run(t, false, "", awsFile())
	if !third.Applied || third.Counts.Unchanged != 3 || !reflect.DeepEqual(e.bank.Bank(e.examID), afterFirst) {
		t.Errorf("re-applying the same file must change nothing: %+v", third.Counts)
	}
}

func TestApplyStoresTheQuestionsAndTheInferredTaxonomy(t *testing.T) { // slice 03 §1.6
	e := newImportEnv(t)

	e.apply(t, awsFile())

	summary, err := e.get.Execute(ctx, e.examID)
	if err != nil {
		t.Fatal(err)
	}
	if summary.QuestionTotal != 3 || len(summary.Classifications) != 2 {
		t.Fatalf("summary = %d questions, %d classifications", summary.QuestionTotal, len(summary.Classifications))
	}
	primary := summary.Classifications[0]
	if primary.Name != "Dominio" || !primary.IsPrimary() || len(primary.Categories) != 3 {
		t.Errorf("primary = %+v", primary)
	}
	weights := []int{primary.Categories[0].WeightPercent, primary.Categories[1].WeightPercent, primary.Categories[2].WeightPercent}
	if !slices.Equal(weights, []int{34, 33, 33}) {
		t.Errorf("weights = %v, want 34/33/33", weights)
	}
	if summary.Classifications[1].Name != "Servicio" || summary.Classifications[1].IsPrimary() {
		t.Errorf("secondary = %+v", summary.Classifications[1])
	}
}

func TestApplyOfAFileWithAnInvalidQuestionWritesNothing(t *testing.T) { // AC-4
	e := newImportEnv(t)
	bad := fq("bad")
	bad.Options = []domain.Option{{Text: "a", Correct: true}, {Text: "b", Correct: true}, {Text: "c"}}
	file := fileOf(fq("one", cn("Dominio", "A")), fq("two", cn("Dominio", "A")), fq("three", cn("Dominio", "B")), bad)

	r := e.run(t, false, "", file)

	if r.Applied {
		t.Error("applied = true for a file with errors")
	}
	if len(r.Errors) != 1 || r.Errors[0].Path != "questions[3].options" || r.Errors[0].Rule != "correct_count" {
		t.Errorf("errors = %+v, want one correct_count at questions[3].options", r.Errors)
	}
	if got := e.bank.Bank(e.examID); len(got.Questions) != 0 || len(got.Classifications) != 0 || e.bank.Applied != 0 {
		t.Errorf("nothing may be written: %+v", got)
	}
}

func TestDuplicateKeyInTheFileIsRejectedAtTheSecondOccurrence(t *testing.T) { // S03-4
	e := newImportEnv(t)

	r := e.run(t, true, "", fileOf(fq("a"), fq("x"), fq("c"), fq("x")))

	if len(r.Errors) != 1 || r.Errors[0].Path != "questions[3].key" || r.Errors[0].Rule != "duplicate_key" {
		t.Errorf("errors = %+v", r.Errors)
	}
}

func TestAThirdClassificationIsRejectedAtTheFirstQuestionThatUsesIt(t *testing.T) { // S03-5
	e := newImportEnv(t)
	e.apply(t, fileOf(fq("a", cn("Dominio", "X"), cn("Servicio", "S"))))

	r := e.run(t, true, "", fileOf(fq("b", cn("Dominio", "X")), fq("c", cn("Nivel", "Alto")), fq("d", cn("Nivel", "Bajo"))))

	if len(r.Errors) != 1 || r.Errors[0].Path != "questions[1].categories" || r.Errors[0].Rule != "too_many_classifications" {
		t.Errorf("errors = %+v", r.Errors)
	}
}

func TestPrimaryParameterAndDefaultOnAFirstImport(t *testing.T) { // S03-6
	file := fileOf(fq("a", cn("Dominio", "Seguridad"), cn("Servicio", "IAM", "S3")), fq("b", cn("Dominio", "Costos"), cn("Servicio", "S3")))

	t.Run("primary=Servicio rejects the question with two services", func(t *testing.T) {
		e := newImportEnv(t)
		r := e.run(t, false, "Servicio", file)
		if len(r.Errors) != 1 || r.Errors[0].Path != "questions[0].categories" || r.Errors[0].Rule != "primary_single_valued" || r.Applied {
			t.Errorf("applied %v errors %+v", r.Applied, r.Errors)
		}
	})
	t.Run("without the parameter Dominio is chosen and the import passes", func(t *testing.T) {
		e := newImportEnv(t)
		r := e.run(t, false, "", file)
		if len(r.Errors) != 0 || !r.Applied || r.Primary == nil || *r.Primary != "Dominio" {
			t.Errorf("applied %v errors %+v primary %v", r.Applied, r.Errors, r.Primary)
		}
	})
}

func TestTwoCategoriesInTheExistingPrimaryAreRejected(t *testing.T) { // AC-5
	e := newImportEnv(t)
	e.apply(t, fileOf(fq("a", cn("Dominio", "Seguridad"))))

	r := e.run(t, false, "", fileOf(fq("b", cn("Dominio", "Seguridad", "Costos"))))

	if len(r.Errors) != 1 || r.Errors[0].Rule != "primary_single_valued" || r.Applied {
		t.Errorf("applied %v errors %+v", r.Applied, r.Errors)
	}
	if got := e.bank.Bank(e.examID); len(got.Questions) != 1 {
		t.Errorf("the bank must keep only the first import: %d questions", len(got.Questions))
	}
}

func TestSimilarCategoryNameIsAWarningNotAnError(t *testing.T) { // AC-3
	e := newImportEnv(t)
	e.apply(t, fileOf(fq("a", cn("Dominio", "Alto rendimiento"))))

	r := e.run(t, true, "", fileOf(fq("a", cn("Dominio", "Alto rendimiento")), fq("b", cn("Dominio", "Alto rendimeinto"))))

	if len(r.Errors) != 0 || len(r.NewCategories) != 1 || !slices.Equal(r.NewCategories[0].SimilarTo, []string{"Alto rendimiento"}) {
		t.Errorf("errors %+v new categories %+v", r.Errors, r.NewCategories)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "is similar to") {
		t.Errorf("warnings = %v", r.Warnings)
	}
}

func TestInferredWeightsAreStoredByLargestRemainder(t *testing.T) { // AC-6
	e := newImportEnv(t)
	var qs []domain.FileQuestion
	for i, n := range []int{5, 3, 3} {
		for j := 0; j < n; j++ {
			qs = append(qs, fq(fmt.Sprintf("q%d-%d", i, j), cn("Dominio", fmt.Sprintf("Cat %c", 'A'+i))))
		}
	}

	e.apply(t, fileOf(qs...))

	summary, _ := e.get.Execute(ctx, e.examID)
	var got []int
	for _, c := range summary.Classifications[0].Categories {
		got = append(got, c.WeightPercent)
	}
	if !slices.Equal(got, []int{46, 27, 27}) {
		t.Errorf("stored weights = %v, want 46/27/27", got)
	}
}

func TestManualWeightsSurviveAnImportAndTheNewCategoryGetsZero(t *testing.T) { // AC-7
	e := newImportEnv(t)
	var qs []domain.FileQuestion
	for i := 0; i < 4; i++ {
		qs = append(qs, fq(fmt.Sprintf("q%d", i), cn("Dominio", fmt.Sprintf("Cat %c", 'A'+i))))
	}
	e.apply(t, fileOf(qs...))
	bank := e.bank.Bank(e.examID)
	for i, w := range []int{30, 26, 24, 20} {
		bank.Classifications[0].Categories[i].WeightPercent = w
	}
	bank.Classifications[0].WeightMode = domain.WeightManual
	e.bank.Set(e.examID, bank)

	r := e.apply(t, fileOf(fq("migr", cn("Dominio", "Migración"))))

	summary, _ := e.get.Execute(ctx, e.examID)
	var got []int
	for _, c := range summary.Classifications[0].Categories {
		got = append(got, c.WeightPercent)
	}
	if !slices.Equal(got, []int{30, 26, 24, 20, 0}) || summary.Classifications[0].WeightMode != domain.WeightManual {
		t.Errorf("weights = %v mode %s, want 30/26/24/20/0 manual", got, summary.Classifications[0].WeightMode)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "is not part of the full simulation until you assign it a weight") {
		t.Errorf("warnings = %v", r.Warnings)
	}
}

func TestReimportingAKeyWithEditedTextKeepsTheIdAndBumpsTheVersion(t *testing.T) { // AC-8
	e := newImportEnv(t)
	e.apply(t, awsFile())
	before := e.bank.Bank(e.examID).Questions[1]
	edited := awsFile()
	edited.Questions[1].Stem = "A rewritten stem"

	r := e.apply(t, edited)

	if r.Counts != (domain.ImportCounts{Update: 1, Unchanged: 2}) {
		t.Errorf("counts = %+v", r.Counts)
	}
	after := e.bank.Bank(e.examID).Questions[1]
	if after.ID != before.ID || after.Version != before.Version+1 || after.Stem != "A rewritten stem" {
		t.Errorf("before %+v after %+v", before, after)
	}
	if got := e.bank.Bank(e.examID); len(got.Questions) != 3 {
		t.Errorf("questions = %d, want 3", len(got.Questions))
	}
}

func TestANewKeyWithTheSameNormalizedStemIsCreatedAndListedAsDuplicate(t *testing.T) { // AC-9
	e := newImportEnv(t)
	first := fq("td-1")
	first.Stem = "Which Route 53 setting controls caching?"
	e.apply(t, fileOf(first))
	dup := fq("wz-1")
	dup.Stem = "which  route 53 SETTING controls caching?  "

	r := e.apply(t, fileOf(dup))

	if r.Counts.Create != 1 || !slices.Equal(r.DuplicateStems, []domain.DuplicateStem{{Key: "wz-1", ExistingKey: "td-1"}}) {
		t.Errorf("counts %+v duplicates %+v", r.Counts, r.DuplicateStems)
	}
	if got := e.bank.Bank(e.examID); len(got.Questions) != 2 {
		t.Errorf("both questions must exist: %d", len(got.Questions))
	}
}

func TestAnyMediaReferenceIsMediaMissingAndNothingIsWritten(t *testing.T) { // slice 03 §1.3 stage 4
	e := newImportEnv(t)
	q := fq("moto")
	q.Stem = "¿Qué indica esta señal? ![Disco rojo](media:img/entrada-prohibida.png)"
	q.Options[0].Image, q.Options[0].Alt = "media:img/ceda.png", "triángulo"

	r := e.run(t, false, "", fileOf(q))

	if r.Applied || len(r.Errors) != 2 || r.Errors[0].Rule != "media_missing" {
		t.Fatalf("applied %v errors %+v", r.Applied, r.Errors)
	}
	if r.Media.Referenced != 2 || !slices.Equal(r.Media.Missing, []string{"img/entrada-prohibida.png", "img/ceda.png"}) {
		t.Errorf("media = %+v", r.Media)
	}
	if len(e.bank.Bank(e.examID).Questions) != 0 {
		t.Error("nothing may be written")
	}
}

func TestFileErrorsFromTheEarlierStagesBecomeTheReportAndWriteNothing(t *testing.T) { // stages 1-2 arrive from the adapter
	e := newImportEnv(t)
	fileErrors := []domain.ImportError{
		{Path: "questions[2].key", Rule: "schema", Message: "does not match pattern"},
		{Path: "questions[2].type", Rule: "schema", Message: "must be single or multi"},
		{Path: "format", Rule: "schema", Message: "value must be 'afwilto-questions'"},
	}

	for _, dry := range []bool{true, false} {
		r, err := e.imp.Execute(ctx, in.ImportQuestionsCommand{ExamID: e.examID, DryRun: dry, FileErrors: fileErrors})

		if err != nil {
			t.Fatal(err)
		}
		if r.DryRun != dry || r.Applied || !reflect.DeepEqual(r.Errors, fileErrors) || r.Counts.Invalid != 1 {
			t.Errorf("dry=%v: %+v", dry, r)
		}
	}
	if e.bank.Applied != 0 {
		t.Error("nothing may be written")
	}
}

func TestImportIntoAnExamThatIsNotAliveIsNotFound(t *testing.T) {
	e := newImportEnv(t)
	deleted := e.add(t, subjectA, "Old")
	if err := e.delete.Execute(ctx, deleted.ID); err != nil {
		t.Fatal(err)
	}
	for name, id := range map[string]string{"unknown": unknownID, "deleted": deleted.ID} {
		for _, dry := range []bool{true, false} {
			for _, cmd := range []in.ImportQuestionsCommand{
				{ExamID: id, DryRun: dry, File: awsFile()},
				{ExamID: id, DryRun: dry, FileErrors: []domain.ImportError{{Path: "$", Rule: "json", Message: "x"}}},
			} {
				if _, err := e.imp.Execute(ctx, cmd); err == nil || !isNotFound(err) {
					t.Errorf("%s exam, dry=%v: err = %v, want not found", name, dry, err)
				}
			}
		}
	}
}

func TestDeletingAnExamWithAliveQuestionsIsBlocked(t *testing.T) { // S03-9, BR-E3
	e := newImportEnv(t)
	e.apply(t, awsFile())

	err := e.delete.Execute(ctx, e.examID)

	wantConflict(t, err, "in_use")
	if _, getErr := e.get.Execute(ctx, e.examID); getErr != nil {
		t.Errorf("the exam must still be alive: %v", getErr)
	}
}

func TestDeletingAnExamWithoutQuestionsStillWorks(t *testing.T) { // S02 behaviour kept
	e := newImportEnv(t)

	if err := e.delete.Execute(ctx, e.examID); err != nil {
		t.Fatal(err)
	}
}

func TestQuestionTotalInTheExamListCountsTheBank(t *testing.T) { // slice 02 §1 "questionTotal ... filled in by slice 03"
	e := newImportEnv(t)
	e.apply(t, awsFile())

	list, err := e.list.Execute(ctx, subjectA)

	if err != nil || len(list) != 1 || list[0].QuestionTotal != 3 {
		t.Errorf("list = %+v, %v", list, err)
	}
}

func TestMalformedCommandWithoutFileOrErrorsIsAProgrammingError(t *testing.T) {
	e := newImportEnv(t)

	_, err := e.imp.Execute(ctx, in.ImportQuestionsCommand{ExamID: e.examID})

	if err == nil {
		t.Error("an import without a file and without file errors must fail loudly")
	}
}

// refusingBank plans like the real one, then fails the write the way Postgres
// does for text it cannot store.
type refusingBank struct{ *testsupport.FakeQuestionBank }

func (b refusingBank) ApplyImport(ctx context.Context, examID string, plan func(domain.Bank) (*domain.ImportWrite, error)) error {
	if err := b.FakeQuestionBank.ApplyImport(ctx, examID, plan); err != nil {
		return err
	}
	return domain.ErrInvalidText
}

func TestApplyAnswersAReportNotAnErrorWhenTheDatabaseRefusesTheText(t *testing.T) { // H3: SQLSTATE 22021 / 22P05 are 422, never 500
	e := newImportEnv(t)
	imp := NewImportQuestionsService(refusingBank{e.bank}, &testsupport.StepClock{}, &testsupport.SeqIDGen{N: 5000})

	report, err := imp.Execute(ctx, in.ImportQuestionsCommand{ExamID: e.examID, File: awsFile()})

	if err != nil {
		t.Fatalf("Execute = %v, want a report", err)
	}
	if report.Applied || len(report.Errors) != 1 || report.Errors[0].Rule != domain.RuleInvalidText || report.Errors[0].Path != "$" {
		t.Errorf("report = applied %v, errors %+v; want one invalid_text error at $", report.Applied, report.Errors)
	}
}
