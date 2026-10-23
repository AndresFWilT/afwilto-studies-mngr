package domain

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"
)

var planNow = time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)

type ids struct{ n int }

func (g *ids) next() string { g.n++; return fmt.Sprintf("id-%03d", g.n) }

// C lists the category names of a question in one classification.
func C(classification string, names ...string) ClassifiedNames {
	return ClassifiedNames{Classification: classification, Names: names}
}

// fq builds a valid single question with 3 options (the second is correct).
func fq(key string, categories ...ClassifiedNames) FileQuestion {
	return FileQuestion{
		Key: key, Type: QuestionSingle, Stem: "Stem of " + key,
		Options: []Option{
			{Text: "wrong one", Rationale: "why not"},
			{Text: "right one", Correct: true, Rationale: "why yes"},
			{Text: "wrong two"},
		},
		Categories: categories,
	}
}

func file(questions ...FileQuestion) QuestionFile {
	return QuestionFile{Source: &Source{Kind: "own", Name: "fixture"}, Questions: questions}
}

func planOf(t *testing.T, bank Bank, f QuestionFile, primary string) ImportPlan {
	t.Helper()
	g := &ids{}
	if bank.Questions != nil || bank.Classifications != nil {
		g.n = 500 // keep new ids apart from the ones a previous plan handed out
	}
	return PlanImport(PlanInput{ExamID: "exam-1", Bank: bank, File: f, Primary: primary, NewID: g.next, Now: planNow})
}

// apply plans, requires success and returns the bank after the write.
func apply(t *testing.T, bank Bank, f QuestionFile, primary string) Bank {
	t.Helper()
	plan := planOf(t, bank, f, primary)
	if len(plan.Report.Errors) != 0 || plan.Write == nil {
		t.Fatalf("apply: errors %+v", plan.Report.Errors)
	}
	return bank.After(*mustWrite(t, plan))
}

func mustWrite(t *testing.T, plan ImportPlan) *ImportWrite {
	t.Helper()
	if plan.Write == nil {
		t.Fatalf("no write; errors = %+v", plan.Report.Errors)
	}
	return plan.Write
}

func errorAt(errs []ImportError, path, rule string) bool {
	for _, e := range errs {
		if e.Path == path && e.Rule == rule && e.Message != "" {
			return true
		}
	}
	return false
}

func pathsOf(errs []ImportError) []string {
	out := make([]string, len(errs))
	for i, e := range errs {
		out[i] = e.Path + "|" + e.Rule
	}
	return out
}

// awsLike mirrors api/examples/aws-text.json (AC-1).
func awsLike() QuestionFile {
	return file(
		fq("example-aws-0001", C("Dominio", "Arquitecturas seguras"), C("Servicio", "IAM", "Amazon S3")),
		fq("example-aws-0002", C("Dominio", "Alto rendimiento"), C("Servicio", "Amazon S3")),
		fq("example-aws-0003", C("Dominio", "Arquitecturas resilientes"), C("Servicio", "Route 53")),
	)
}

func TestPlanImportOnAnEmptyExamInfersClassificationsCategoriesPrimaryAndWeights(t *testing.T) { // AC-1
	plan := planOf(t, Bank{}, awsLike(), "")

	r := plan.Report
	if len(r.Errors) != 0 {
		t.Fatalf("errors = %+v", r.Errors)
	}
	if r.Counts != (ImportCounts{Create: 3}) || !slices.Equal(r.Keys.Create, []string{"example-aws-0001", "example-aws-0002", "example-aws-0003"}) {
		t.Errorf("counts = %+v keys = %+v", r.Counts, r.Keys)
	}
	if r.Primary == nil || *r.Primary != "Dominio" {
		t.Errorf("primary = %v, want Dominio (Servicio has a 2-valued question)", r.Primary)
	}
	if !slices.Equal(r.NewClassifications, []string{"Dominio", "Servicio"}) {
		t.Errorf("new classifications = %v", r.NewClassifications)
	}
	var got []string
	for _, c := range r.NewCategories {
		got = append(got, c.Classification+"/"+c.Name)
		if len(c.SimilarTo) != 0 {
			t.Errorf("%s/%s: unexpected similarTo %v", c.Classification, c.Name, c.SimilarTo)
		}
	}
	want := []string{"Dominio/Arquitecturas seguras", "Servicio/IAM", "Servicio/Amazon S3", "Dominio/Alto rendimiento", "Dominio/Arquitecturas resilientes", "Servicio/Route 53"}
	if !slices.Equal(got, want) {
		t.Errorf("new categories (first appearance order) = %v, want %v", got, want)
	}
	wantWeights := []WeightAfter{
		{"Arquitecturas seguras", 34, WeightInferred}, {"Alto rendimiento", 33, WeightInferred}, {"Arquitecturas resilientes", 33, WeightInferred},
	}
	if !slices.Equal(r.WeightsAfter, wantWeights) {
		t.Errorf("weightsAfter = %+v, want %+v", r.WeightsAfter, wantWeights)
	}
	if len(r.Preview) != 3 || len(r.DuplicateStems) != 0 || len(r.Warnings) != 0 {
		t.Errorf("preview %d, duplicates %v, warnings %v", len(r.Preview), r.DuplicateStems, r.Warnings)
	}
	if r.Media.Referenced != 0 || r.Media.InZip != 0 || len(r.Media.Missing) != 0 || len(r.Media.Unused) != 0 {
		t.Errorf("media = %+v", r.Media)
	}
	if r.DryRun || r.Applied {
		t.Error("PlanImport leaves dryRun and applied to the caller")
	}
}

func TestPlanImportWriteHoldsWhatApplyPersists(t *testing.T) { // slice 03 §1.6
	plan := planOf(t, Bank{}, awsLike(), "")

	w := plan.Write
	if w == nil {
		t.Fatal("no write for a clean plan")
	}
	if w.ExamID != "exam-1" || !w.Now.Equal(planNow) || w.PromoteToPrimary != "" {
		t.Errorf("write header = %+v", w)
	}
	if len(w.Classifications) != 2 || w.Classifications[0].Name != "Dominio" || w.Classifications[0].Position != PositionPrimary ||
		w.Classifications[1].Name != "Servicio" || w.Classifications[1].Position != PositionSecondary ||
		w.Classifications[0].WeightMode != WeightInferred {
		t.Errorf("classifications = %+v", w.Classifications)
	}
	if len(w.Categories) != 6 || w.Categories[0].Key != "arquitecturas-seguras" || w.Categories[0].Fold != "arquitecturas seguras" ||
		w.Categories[0].ClassificationID != w.Classifications[0].ID || w.Categories[1].ClassificationID != w.Classifications[1].ID {
		t.Errorf("categories = %+v", w.Categories)
	}
	if len(w.Creates) != 3 || len(w.Updates) != 0 {
		t.Fatalf("creates %d updates %d", len(w.Creates), len(w.Updates))
	}
	first := w.Creates[0]
	if first.Question.ID == "" || first.Question.ExamID != "exam-1" || first.Question.Version != 1 ||
		!first.Question.CreatedAt.Equal(planNow) || !first.Question.UpdatedAt.Equal(planNow) ||
		first.Question.Fingerprint != StemFingerprint("Stem of example-aws-0001") {
		t.Errorf("created question = %+v", first.Question)
	}
	if len(first.CategoryIDs) != 3 {
		t.Errorf("category ids = %v, want the Dominio category and the IAM and S3 services", first.CategoryIDs)
	}
	if len(w.Weights) != 3 {
		t.Errorf("weights = %+v, want one per primary category", w.Weights)
	}
	seen := map[string]bool{}
	for _, id := range []string{w.Classifications[0].ID, w.Classifications[1].ID, first.Question.ID} {
		if id == "" || seen[id] {
			t.Errorf("ids must be distinct and non-empty: %q", id)
		}
		seen[id] = true
	}
}

func TestPlanImportDuplicateKeysAreReportedOnTheRepeatedOccurrence(t *testing.T) { // S03-4
	f := file(fq("a"), fq("x"), fq("c"), fq("x"))

	plan := planOf(t, Bank{}, f, "")

	if got := pathsOf(plan.Report.Errors); !slices.Equal(got, []string{"questions[3].key|duplicate_key"}) {
		t.Errorf("errors = %v, want exactly questions[3].key|duplicate_key", got)
	}
	if plan.Write != nil || plan.Report.Counts.Invalid != 1 || plan.Report.Counts.Create != 0 {
		t.Errorf("write %v, counts %+v", plan.Write, plan.Report.Counts)
	}
}

func TestPlanImportSemanticsCollectEveryError(t *testing.T) { // each stage collects all of its errors; AC-4 path
	bad := fq("b")
	bad.Options = []Option{{Text: "a", Correct: true}, {Text: "b", Correct: true}, {Text: "c"}}
	f := file(fq("a"), fq("a2"), fq("a3"), bad, fq("a"), fq("Bad Key"))

	plan := planOf(t, Bank{}, f, "")

	got := pathsOf(plan.Report.Errors)
	for _, want := range []string{"questions[3].options|correct_count", "questions[4].key|duplicate_key", "questions[5].key|pattern"} {
		if !slices.Contains(got, want) {
			t.Errorf("errors = %v, want %s among them", got, want)
		}
	}
	if plan.Report.Counts.Invalid != 3 {
		t.Errorf("invalid = %d, want 3 distinct questions", plan.Report.Counts.Invalid)
	}
}

func TestPlanImportRejectsAThirdClassificationAtTheFirstQuestionThatUsesIt(t *testing.T) { // S03-5, BR-C1
	bank := Bank{Classifications: []Classification{
		{ID: "c1", Name: "Dominio", Fold: "dominio", Position: PositionPrimary, WeightMode: WeightInferred},
		{ID: "c2", Name: "Servicio", Fold: "servicio", Position: PositionSecondary, WeightMode: WeightInferred},
	}}
	f := file(fq("a", C("Dominio", "X")), fq("b", C("Dominio", "X"), C("Nivel", "Alto")), fq("c", C("Nivel", "Bajo")))

	plan := planOf(t, bank, f, "")

	if got := pathsOf(plan.Report.Errors); !slices.Equal(got, []string{"questions[1].categories|too_many_classifications"}) {
		t.Errorf("errors = %v, want one too_many_classifications at questions[1].categories", got)
	}
	if plan.Write != nil {
		t.Error("an invalid plan must carry no write")
	}
}

func TestPlanImportRejectsAThirdClassificationWithinAFirstImport(t *testing.T) { // BR-C1 on an empty exam
	f := file(fq("a", C("A", "x"), C("B", "y")), fq("b", C("A", "x"), C("C", "z")))

	plan := planOf(t, Bank{}, f, "")

	if got := pathsOf(plan.Report.Errors); !slices.Equal(got, []string{"questions[1].categories|too_many_classifications"}) {
		t.Errorf("errors = %v", got)
	}
}

func TestPlanImportNamesThatDifferOnlyInCaseAndAccentsAreTheSameClassification(t *testing.T) { // FR-3.1, BR-C1
	bank := Bank{Classifications: []Classification{
		{ID: "c1", Name: "Dominio", Fold: "dominio", Position: PositionPrimary, WeightMode: WeightInferred},
		{ID: "c2", Name: "Módulo", Fold: "modulo", Position: PositionSecondary, WeightMode: WeightInferred},
	}}
	f := file(fq("a", C("  DOMINIO ", "X"), C("modulo", "Y")))

	plan := planOf(t, bank, f, "")

	if len(plan.Report.Errors) != 0 || len(plan.Report.NewClassifications) != 0 {
		t.Errorf("errors %+v, new classifications %v", plan.Report.Errors, plan.Report.NewClassifications)
	}
	if mustWrite(t, plan).Creates[0].Question.Categories[0].Classification != "Dominio" {
		t.Errorf("stored classification name = %q, want the existing spelling", mustWrite(t, plan).Creates[0].Question.Categories[0].Classification)
	}
}

func servicesFile() QuestionFile {
	return file(
		fq("a", C("Dominio", "Seguridad"), C("Servicio", "IAM", "S3")), // two services
		fq("b", C("Dominio", "Costos"), C("Servicio", "S3")),
	)
}

func TestPlanImportPrimaryParameterMakesTheMultiValuedQuestionInvalid(t *testing.T) { // S03-6
	plan := planOf(t, Bank{}, servicesFile(), "Servicio")

	if got := pathsOf(plan.Report.Errors); !slices.Equal(got, []string{"questions[0].categories|primary_single_valued"}) {
		t.Errorf("errors = %v, want questions[0].categories|primary_single_valued", got)
	}
}

func TestPlanImportWithoutTheParameterPicksTheFirstSingleValuedClassification(t *testing.T) { // S03-6
	plan := planOf(t, Bank{}, servicesFile(), "")

	if len(plan.Report.Errors) != 0 || plan.Report.Primary == nil || *plan.Report.Primary != "Dominio" {
		t.Errorf("errors %+v primary %v, want a clean import with Dominio", plan.Report.Errors, plan.Report.Primary)
	}
}

func TestPlanImportParameterIsMatchedByFold(t *testing.T) { // FR-3.1
	f := file(fq("a", C("Dominio", "X"), C("Módulo", "Y")), fq("b", C("Dominio", "X"), C("Módulo", "Z")))

	plan := planOf(t, Bank{}, f, " MODULO ")

	if plan.Report.Primary == nil || *plan.Report.Primary != "Módulo" || mustWrite(t, plan).Classifications[1].Position != PositionPrimary {
		t.Errorf("primary %v classifications %+v", plan.Report.Primary, mustWrite(t, plan).Classifications)
	}
}

func TestPlanImportAnUnknownPrimaryParameterOnAFirstImportIsAnError(t *testing.T) { // FR-3.1 + contract: the parameter names a classification
	plan := planOf(t, Bank{}, servicesFile(), "Nivel")

	if got := pathsOf(plan.Report.Errors); !slices.Equal(got, []string{"primary|not_found"}) {
		t.Errorf("errors = %v, want primary|not_found", got)
	}
}

func TestPlanImportParameterIsIgnoredOnceTheExamHasAPrimary(t *testing.T) { // FR-3.1: "Once the exam has a primary, primary is ignored"
	bank := apply(t, Bank{}, servicesFile(), "")

	plan := planOf(t, bank, servicesFile(), "Servicio")
	unknown := planOf(t, bank, servicesFile(), "Nivel")

	for name, p := range map[string]ImportPlan{"other classification": plan, "unknown name": unknown} {
		if len(p.Report.Errors) != 0 || p.Report.Primary == nil || *p.Report.Primary != "Dominio" {
			t.Errorf("%s: errors %+v primary %v, want a clean import with the existing primary Dominio", name, p.Report.Errors, p.Report.Primary)
		}
	}
}

func TestPlanImportRejectsTwoCategoriesInTheExistingPrimary(t *testing.T) { // AC-5, BR-C2
	bank := apply(t, Bank{}, file(fq("a", C("Dominio", "Seguridad"))), "")
	f := file(fq("a", C("Dominio", "Seguridad")), fq("b", C("Dominio", "Seguridad", "Costos")))

	plan := planOf(t, bank, f, "")

	if got := pathsOf(plan.Report.Errors); !slices.Equal(got, []string{"questions[1].categories|primary_single_valued"}) {
		t.Errorf("errors = %v", got)
	}
}

func TestPlanImportTwoSpellingsOfTheSameCategoryAreOneValueNotTwo(t *testing.T) { // BR-C2 counts folded categories
	f := file(fq("a", C("Dominio", "Alto rendimiento", "ALTO   rendimiento")))

	plan := planOf(t, Bank{}, f, "")

	if len(plan.Report.Errors) != 0 {
		t.Errorf("errors = %+v", plan.Report.Errors)
	}
	if len(plan.Report.NewCategories) != 1 {
		t.Errorf("new categories = %+v, want one", plan.Report.NewCategories)
	}
}

func TestPlanImportEligibilityLooksAtQuestionsAlreadyInTheBank(t *testing.T) { // BR-C2 over the final bank
	// Both classifications exist, none is primary (no eligible one at the first import).
	first := file(fq("a", C("Dominio", "X", "Y"), C("Servicio", "S", "T")))
	bank := apply(t, Bank{}, first, "")
	for _, c := range bank.Classifications {
		if c.IsPrimary() {
			t.Fatalf("test setup: %s became primary", c.Name)
		}
	}
	// A second file where every question has one value in both: the bank's question a still has 2 in each,
	// so neither classification may be chosen by default.
	plan := planOf(t, bank, file(fq("b", C("Dominio", "X"), C("Servicio", "S"))), "")

	if plan.Report.Primary != nil || len(plan.Report.Errors) != 0 {
		t.Errorf("primary %v errors %+v, want no primary and no error", plan.Report.Primary, plan.Report.Errors)
	}
}

func TestPlanImportWithNoEligibleClassificationLeavesTheExamWithoutPrimaryAndWarns(t *testing.T) { // FR-3.1 rule 4
	f := file(fq("a", C("Dominio", "X", "Y"), C("Servicio", "S", "T")))

	plan := planOf(t, Bank{}, f, "")

	r := plan.Report
	if len(r.Errors) != 0 || r.Primary != nil {
		t.Fatalf("errors %+v primary %v", r.Errors, r.Primary)
	}
	if len(r.WeightsAfter) != 0 || len(mustWrite(t, plan).Weights) != 0 {
		t.Errorf("no primary, no weights: %+v / %+v", r.WeightsAfter, mustWrite(t, plan).Weights)
	}
	if len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "clasificación principal") {
		t.Errorf("warnings = %v, want one about the missing primary classification", r.Warnings)
	}
	for _, c := range mustWrite(t, plan).Classifications {
		if c.Position != PositionSecondary {
			t.Errorf("%s has position %d, want secondary while there is no primary", c.Name, c.Position)
		}
	}
}

func TestPlanImportPromotesAnExistingSecondaryWhenTheParameterNamesIt(t *testing.T) { // FR-3.1 rule 2 after an unresolved first import
	bank := apply(t, Bank{}, file(fq("a", C("Dominio", "X"), C("Servicio", "S", "T")), fq("b", C("Servicio", "S"))), "") // Dominio not used by b
	if bank.Classifications[0].IsPrimary() || bank.Classifications[1].IsPrimary() {
		t.Fatal("test setup: expected no primary")
	}

	plan := planOf(t, bank, file(fq("c", C("Dominio", "X")), fq("d", C("Dominio", "Y"))), "")

	if plan.Report.Primary == nil || *plan.Report.Primary != "Dominio" {
		t.Fatalf("primary %v errors %+v, want Dominio now that every question of the file uses it once", plan.Report.Primary, plan.Report.Errors)
	}
	if mustWrite(t, plan).PromoteToPrimary != bank.Classifications[0].ID {
		t.Errorf("promote = %q, want the existing Dominio %q", mustWrite(t, plan).PromoteToPrimary, bank.Classifications[0].ID)
	}
	after := bank.After(*mustWrite(t, plan))
	if !after.Classifications[0].IsPrimary() || after.Classifications[1].IsPrimary() {
		t.Errorf("positions after: %+v", after.Classifications)
	}
}

func TestPlanImportFlagsNewCategoriesThatResembleExistingOnes(t *testing.T) { // AC-3
	bank := apply(t, Bank{}, file(fq("a", C("Dominio", "Alto rendimiento"))), "")
	f := file(fq("a", C("Dominio", "Alto rendimiento")), fq("b", C("Dominio", "Alto rendimeinto")))

	plan := planOf(t, bank, f, "")

	r := plan.Report
	if len(r.NewCategories) != 1 || r.NewCategories[0].Name != "Alto rendimeinto" || r.NewCategories[0].Classification != "Dominio" ||
		!slices.Equal(r.NewCategories[0].SimilarTo, []string{"Alto rendimiento"}) {
		t.Fatalf("new categories = %+v", r.NewCategories)
	}
	if len(r.Warnings) != 1 || r.Warnings[0] != `"Alto rendimeinto" se parece a "Alto rendimiento"` {
		t.Errorf("warnings = %q", r.Warnings)
	}
	if len(plan.Report.Errors) != 0 {
		t.Errorf("a similar name is a warning, not an error: %+v", r.Errors)
	}
}

func TestPlanImportNeverMergesSimilarCategoriesOnItsOwn(t *testing.T) { // FR-3.2
	bank := apply(t, Bank{}, file(fq("a", C("Dominio", "Alto rendimiento"))), "")

	plan := planOf(t, bank, file(fq("b", C("Dominio", "Alto rendimeinto"))), "")

	if len(mustWrite(t, plan).Categories) != 1 || mustWrite(t, plan).Categories[0].Name != "Alto rendimeinto" {
		t.Errorf("categories written = %+v, want the new spelling kept as its own category", mustWrite(t, plan).Categories)
	}
}

func TestPlanImportMatchesCategoriesByFoldAndKeepsTheStoredSpelling(t *testing.T) { // FR-3.2
	bank := apply(t, Bank{}, file(fq("a", C("Dominio", "Señales"))), "")

	plan := planOf(t, bank, file(fq("b", C("dominio", "  SENALES "))), "")

	if len(plan.Report.NewCategories) != 0 || len(mustWrite(t, plan).Categories) != 0 {
		t.Fatalf("new categories %+v written %+v, want none", plan.Report.NewCategories, mustWrite(t, plan).Categories)
	}
	if got := mustWrite(t, plan).Creates[0].Question.Categories; len(got) != 1 || got[0] != (CategoryRef{"Dominio", "Señales"}) {
		t.Errorf("stored categories = %+v, want the existing spelling", got)
	}
}

func counts(n ...int) []FileQuestion {
	var qs []FileQuestion
	for i, c := range n {
		for j := 0; j < c; j++ {
			qs = append(qs, fq(fmt.Sprintf("c%d-%d", i, j), C("Dominio", fmt.Sprintf("Cat %c", 'A'+i))))
		}
	}
	return qs
}

func TestPlanImportInferredWeightsUseLargestRemainder(t *testing.T) { // AC-6
	plan := planOf(t, Bank{}, file(counts(5, 3, 3)...), "")

	want := []WeightAfter{{"Cat A", 46, WeightInferred}, {"Cat B", 27, WeightInferred}, {"Cat C", 27, WeightInferred}}
	if !slices.Equal(plan.Report.WeightsAfter, want) {
		t.Errorf("weightsAfter = %+v, want %+v", plan.Report.WeightsAfter, want)
	}
	if len(mustWrite(t, plan).Weights) != 3 {
		t.Fatalf("write weights = %+v", mustWrite(t, plan).Weights)
	}
	total := 0
	for _, w := range mustWrite(t, plan).Weights {
		total += w.Percent
	}
	if total != 100 {
		t.Errorf("stored weights sum to %d, want 100", total)
	}
}

func TestPlanImportInferredWeightsCountTheWholeBankNotJustTheFile(t *testing.T) { // FR-3.4: recomputed over the alive classified questions
	bank := apply(t, Bank{}, file(counts(5, 3, 3)...), "")
	extra := file(fq("more-1", C("Dominio", "Cat C")), fq("more-2", C("Dominio", "Cat C")), fq("none"))

	plan := planOf(t, bank, extra, "")

	// 5 / 3 / 5 classified (the question with no category is "sin clasificar" and does not count)
	want := []WeightAfter{{"Cat A", 38, WeightInferred}, {"Cat B", 23, WeightInferred}, {"Cat C", 38, WeightInferred}}
	// 100*5/13 = 38.46, 23.08, 38.46 -> floors 38,23,38 = 99; the largest remainder (.46 twice) goes to the lower index
	if !slices.Equal(plan.Report.WeightsAfter, want) {
		t.Errorf("weightsAfter = %+v, want %+v", plan.Report.WeightsAfter, want)
	}
}

func manualBank(t *testing.T) Bank {
	t.Helper()
	bank := apply(t, Bank{}, file(counts(2, 2, 2, 2)...), "")
	for i, w := range []int{30, 26, 24, 20} {
		bank.Classifications[0].Categories[i].WeightPercent = w
	}
	bank.Classifications[0].WeightMode = WeightManual
	return bank
}

func TestPlanImportManualWeightsStayAndANewCategoryGetsZeroWithAWarning(t *testing.T) { // AC-7
	bank := manualBank(t)

	plan := planOf(t, bank, file(fq("migr", C("Dominio", "Migración"))), "")

	want := []WeightAfter{
		{"Cat A", 30, WeightManual}, {"Cat B", 26, WeightManual}, {"Cat C", 24, WeightManual}, {"Cat D", 20, WeightManual}, {"Migración", 0, WeightManual},
	}
	if !slices.Equal(plan.Report.WeightsAfter, want) {
		t.Errorf("weightsAfter = %+v, want %+v", plan.Report.WeightsAfter, want)
	}
	if len(plan.Report.Warnings) != 1 || plan.Report.Warnings[0] != `"Migración" no entra al simulacro real hasta que le asignes peso` {
		t.Errorf("warnings = %q", plan.Report.Warnings)
	}
	if len(mustWrite(t, plan).Weights) != 0 {
		t.Errorf("manual weights are never rewritten: %+v", mustWrite(t, plan).Weights)
	}
}

func TestPlanImportDuplicateStemsListCreatedQuestionsThatRepeatAnExistingStem(t *testing.T) { // AC-9
	first := fq("td-1")
	first.Stem = "What is the TTL of a record?"
	bank := apply(t, Bank{}, file(first), "")
	dup := fq("wz-1")
	dup.Stem = "  what is  the TTL of a RECORD? "

	plan := planOf(t, bank, file(dup), "")

	if len(plan.Report.Errors) != 0 || plan.Report.Counts.Create != 1 {
		t.Fatalf("a duplicate stem is a warning: errors %+v counts %+v", plan.Report.Errors, plan.Report.Counts)
	}
	if !slices.Equal(plan.Report.DuplicateStems, []DuplicateStem{{Key: "wz-1", ExistingKey: "td-1"}}) {
		t.Errorf("duplicateStems = %+v", plan.Report.DuplicateStems)
	}
}

func TestPlanImportReimportingTheSameKeyIsNotADuplicateStem(t *testing.T) { // AC-2
	bank := apply(t, Bank{}, awsLike(), "")

	plan := planOf(t, bank, awsLike(), "")

	if len(plan.Report.DuplicateStems) != 0 {
		t.Errorf("duplicateStems = %+v, want none", plan.Report.DuplicateStems)
	}
}

func TestPlanImportAQuestionWhoseStemMovesAwayIsNoLongerADuplicate(t *testing.T) { // final-state reading of AC-9
	a := fq("a")
	a.Stem = "Same words"
	bank := apply(t, Bank{}, file(a), "")
	a.Stem = "Other words"
	b := fq("b")
	b.Stem = "Same words"

	plan := planOf(t, bank, file(a, b), "")

	if len(plan.Report.DuplicateStems) != 0 {
		t.Errorf("duplicateStems = %+v: a is rewritten by this very file", plan.Report.DuplicateStems)
	}
}

func fullQuestion() FileQuestion {
	q := fq("full", C("Dominio", "A"), C("Servicio", "X", "Y"))
	q.Explanation = "because"
	q.Difficulty = "medium"
	q.Origin = &Origin{Set: "Set 1", Number: 4}
	return q
}

func TestPlanImportIdenticalContentIsUnchangedEvenWhenSpelledDifferently(t *testing.T) { // AC-2, slice 03 §1.5 canonical form
	bank := apply(t, Bank{}, file(fullQuestion()), "")
	same := map[string]func(q *FileQuestion){
		"identical":                    func(q *FileQuestion) {},
		"category case and accents":    func(q *FileQuestion) { q.Categories = []ClassifiedNames{C("dominio", "a"), C("SERVICIO", "x", "Y")} },
		"category order in the list":   func(q *FileQuestion) { q.Categories[1] = C("Servicio", "Y", "X") },
		"classification order":         func(q *FileQuestion) { q.Categories = []ClassifiedNames{q.Categories[1], q.Categories[0]} },
		"category listed twice":        func(q *FileQuestion) { q.Categories[1] = C("Servicio", "X", "x", "Y") },
		"source restated per question": func(q *FileQuestion) { q.Source = &Source{Kind: "own", Name: "fixture"} },
	}
	for name, change := range same {
		t.Run(name, func(t *testing.T) {
			q := fullQuestion()
			change(&q)

			plan := planOf(t, bank, file(q), "")

			if plan.Report.Counts != (ImportCounts{Unchanged: 1}) || !slices.Equal(plan.Report.Keys.Unchanged, []string{"full"}) {
				t.Errorf("counts = %+v keys = %+v, want 1 unchanged", plan.Report.Counts, plan.Report.Keys)
			}
			if len(mustWrite(t, plan).Creates) != 0 || len(mustWrite(t, plan).Updates) != 0 {
				t.Errorf("an unchanged question writes nothing: %+v", plan.Write)
			}
		})
	}
}

func TestPlanImportAnyContentChangeMakesTheQuestionAnUpdate(t *testing.T) { // slice 03 §1.5: stem, options in order, explanation, difficulty, categories, origin, source
	bank := apply(t, Bank{}, file(fullQuestion()), "")
	changes := map[string]func(q *FileQuestion){
		"stem":                 func(q *FileQuestion) { q.Stem += "!" },
		"stem whitespace":      func(q *FileQuestion) { q.Stem = " " + q.Stem },
		"option text":          func(q *FileQuestion) { q.Options[0].Text = "changed" },
		"option order":         func(q *FileQuestion) { q.Options[0], q.Options[2] = q.Options[2], q.Options[0] },
		"correct flag":         func(q *FileQuestion) { q.Options[1].Correct, q.Options[2].Correct = false, true },
		"option rationale":     func(q *FileQuestion) { q.Options[0].Rationale = "" },
		"an extra option":      func(q *FileQuestion) { q.Options = append(q.Options, Option{Text: "four"}) },
		"explanation changed":  func(q *FileQuestion) { q.Explanation = "other" },
		"explanation removed":  func(q *FileQuestion) { q.Explanation = "" },
		"difficulty changed":   func(q *FileQuestion) { q.Difficulty = "hard" },
		"difficulty removed":   func(q *FileQuestion) { q.Difficulty = "" },
		"origin number":        func(q *FileQuestion) { q.Origin = &Origin{Set: "Set 1", Number: 5} },
		"origin removed":       func(q *FileQuestion) { q.Origin = nil },
		"source kind":          func(q *FileQuestion) { q.Source = &Source{Kind: "purchased", Name: "fixture"} },
		"source name":          func(q *FileQuestion) { q.Source = &Source{Kind: "own", Name: "other"} },
		"a category added":     func(q *FileQuestion) { q.Categories[1] = C("Servicio", "X", "Y", "Z") },
		"a category removed":   func(q *FileQuestion) { q.Categories[1] = C("Servicio", "X") },
		"a category swapped":   func(q *FileQuestion) { q.Categories[0] = C("Dominio", "B") },
		"a classification cut": func(q *FileQuestion) { q.Categories = q.Categories[:1] },
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			q := fullQuestion()
			change(&q)

			plan := planOf(t, bank, file(q), "")

			if len(plan.Report.Errors) != 0 {
				t.Fatalf("errors = %+v", plan.Report.Errors)
			}
			if plan.Report.Counts != (ImportCounts{Update: 1}) || !slices.Equal(plan.Report.Keys.Update, []string{"full"}) {
				t.Errorf("counts = %+v, want 1 update", plan.Report.Counts)
			}
		})
	}
}

func TestPlanImportUpdateKeepsTheIdAndIncrementsTheVersion(t *testing.T) { // AC-8
	bank := apply(t, Bank{}, file(fullQuestion()), "")
	stored := bank.Questions[0]
	q := fullQuestion()
	q.Stem = "edited stem"

	plan := planOf(t, bank, file(q), "")

	if len(mustWrite(t, plan).Updates) != 1 || len(mustWrite(t, plan).Creates) != 0 {
		t.Fatalf("updates %d creates %d", len(mustWrite(t, plan).Updates), len(mustWrite(t, plan).Creates))
	}
	u := mustWrite(t, plan).Updates[0]
	if u.Question.ID != stored.ID || u.Question.Version != stored.Version+1 || u.ExpectedVersion != stored.Version ||
		!u.Question.CreatedAt.Equal(stored.CreatedAt) || !u.Question.UpdatedAt.Equal(planNow) ||
		u.Question.Stem != "edited stem" || u.Question.Fingerprint != StemFingerprint("edited stem") {
		t.Errorf("update = %+v (stored %+v)", u, stored)
	}
	after := bank.After(*mustWrite(t, plan))
	if len(after.Questions) != 1 || after.Questions[0].ID != stored.ID || after.Questions[0].Version != 2 {
		t.Errorf("after = %+v", after.Questions)
	}
}

func TestPlanImportMixesCreateUpdateAndUnchangedInOneFile(t *testing.T) { // AC-2 + AC-8 together
	bank := apply(t, Bank{}, awsLike(), "")
	f := awsLike()
	f.Questions[1].Stem = "changed"
	f.Questions = append(f.Questions, fq("brand-new", C("Dominio", "Arquitecturas seguras")))

	plan := planOf(t, bank, f, "")

	r := plan.Report
	if r.Counts != (ImportCounts{Create: 1, Update: 1, Unchanged: 2}) {
		t.Errorf("counts = %+v", r.Counts)
	}
	if !slices.Equal(r.Keys.Create, []string{"brand-new"}) || !slices.Equal(r.Keys.Update, []string{"example-aws-0002"}) ||
		!slices.Equal(r.Keys.Unchanged, []string{"example-aws-0001", "example-aws-0003"}) {
		t.Errorf("keys = %+v", r.Keys)
	}
	if len(r.NewCategories) != 0 || len(r.NewClassifications) != 0 {
		t.Errorf("nothing new in the taxonomy: %+v %+v", r.NewClassifications, r.NewCategories)
	}
}

func TestPlanImportQuestionsMissingFromTheFileAreKept(t *testing.T) { // FR-4: import is additive
	bank := apply(t, Bank{}, awsLike(), "")

	after := bank.After(*mustWrite(t, planOf(t, bank, file(fq("another")), "")))

	if len(after.Questions) != 4 {
		t.Errorf("questions after = %d, want the 3 old ones plus the new one", len(after.Questions))
	}
}

func TestPlanImportAnyMediaReferenceIsMediaMissingInThisSlice(t *testing.T) { // slice 03 §1.3 stage 4
	q := fq("m")
	q.Stem = "Look ![Señal](media:img/r101.png) and ![again](media:img/r101.png)"
	q.Explanation = "See ![x](media:img/expl.png)"
	q.Options[0].Image, q.Options[0].Alt = "media:img/opt.png", "alt"
	q.Options[1].Rationale = "![r](media:img/why.png)"
	q.Options[2].Text = "plain text mentioning social media: Twitter"

	plan := planOf(t, Bank{}, file(q), "")

	r := plan.Report
	for _, want := range []string{"questions[0].stem", "questions[0].explanation", "questions[0].options[0].image", "questions[0].options[1].rationale"} {
		if !errorAt(r.Errors, want, RuleMediaMissing) {
			t.Errorf("errors = %v, want %s|media_missing", pathsOf(r.Errors), want)
		}
	}
	if errorAt(r.Errors, "questions[0].options[2].text", RuleMediaMissing) {
		t.Error("the words 'social media:' in plain text are not a media reference")
	}
	wantMissing := []string{"img/r101.png", "img/expl.png", "img/opt.png", "img/why.png"}
	if r.Media.Referenced != 4 || r.Media.InZip != 0 || !slices.Equal(r.Media.Missing, wantMissing) || len(r.Media.Unused) != 0 {
		t.Errorf("media = %+v, want 4 referenced and %v missing", r.Media, wantMissing)
	}
	if plan.Write != nil {
		t.Error("an invalid plan must carry no write")
	}
}

func TestPlanImportStopsAtTheFirstFailingStage(t *testing.T) { // slice 03 §1.3
	withMedia := fq("m")
	withMedia.Stem = "![x](media:a.png)"
	f := file(fq("dup"), fq("dup"), withMedia)

	plan := planOf(t, Bank{}, f, "")

	if got := pathsOf(plan.Report.Errors); !slices.Equal(got, []string{"questions[1].key|duplicate_key"}) {
		t.Errorf("errors = %v: the media stage must not run after a semantic failure", got)
	}
	if plan.Report.Media.Referenced != 0 {
		t.Errorf("media = %+v, want it untouched", plan.Report.Media)
	}
}

func TestPlanImportPreviewHoldsTheFirstFiveQuestionsAsTheyWillBeStored(t *testing.T) { // FR-4 preview
	var qs []FileQuestion
	for i := 1; i <= 7; i++ {
		qs = append(qs, fq(fmt.Sprintf("q%d", i), C("Dominio", "señales")))
	}
	bank := apply(t, Bank{}, file(fq("q2", C("Dominio", "Señales"))), "")

	plan := planOf(t, bank, file(qs...), "")

	p := plan.Report.Preview
	if len(p) != 5 {
		t.Fatalf("preview = %d questions, want 5", len(p))
	}
	for i, want := range []string{"q1", "q2", "q3", "q4", "q5"} {
		if p[i].Key != want {
			t.Errorf("preview[%d] = %s, want %s (file order)", i, p[i].Key, want)
		}
	}
	if p[0].Version != 1 || p[0].ID == "" || p[0].Source != (Source{Kind: "own", Name: "fixture"}) || p[0].Categories[0] != (CategoryRef{"Dominio", "Señales"}) {
		t.Errorf("preview[0] = %+v: stored spelling of the category, version 1, resolved source", p[0])
	}
	if p[1].ID != bank.Questions[0].ID {
		t.Errorf("preview[1] is the unchanged stored question: id %q, want %q", p[1].ID, bank.Questions[0].ID)
	}
}

func TestPlanImportFallsBackToTheDefaultSourceWhenNeitherFileNorQuestionHasOne(t *testing.T) { // Question.source is required by the contract
	f := QuestionFile{Questions: []FileQuestion{fq("a")}}

	plan := planOf(t, Bank{}, f, "")

	if got := mustWrite(t, plan).Creates[0].Question.Source; got != DefaultSource {
		t.Errorf("source = %+v, want %+v", got, DefaultSource)
	}
}

func TestPlanImportQuestionSourceOverridesTheFileSource(t *testing.T) { // FR-2
	q := fq("a")
	q.Source = &Source{Kind: "purchased", Name: "Whizlabs"}

	plan := planOf(t, Bank{}, file(q, fq("b")), "")

	if mustWrite(t, plan).Creates[0].Question.Source != (Source{Kind: "purchased", Name: "Whizlabs"}) || mustWrite(t, plan).Creates[1].Question.Source != (Source{Kind: "own", Name: "fixture"}) {
		t.Errorf("sources = %+v / %+v", mustWrite(t, plan).Creates[0].Question.Source, mustWrite(t, plan).Creates[1].Question.Source)
	}
}

func TestPlanImportDoesNotModifyItsInputs(t *testing.T) { // pure function
	bank := apply(t, Bank{}, awsLike(), "")
	f := awsLike()
	f.Questions[0].Stem = "edited"
	before := fmt.Sprintf("%+v|%+v", bank, f)

	planOf(t, bank, f, "")

	if after := fmt.Sprintf("%+v|%+v", bank, f); after != before {
		t.Error("PlanImport mutated its input")
	}
}

func TestBankAfterAppliesAWriteWithoutAliasingTheOriginal(t *testing.T) {
	bank := apply(t, Bank{}, awsLike(), "")
	before := fmt.Sprintf("%+v", bank)

	plan := planOf(t, bank, file(fq("new", C("Dominio", "Fresh"))), "")
	after := bank.After(*mustWrite(t, plan))

	if fmt.Sprintf("%+v", bank) != before {
		t.Error("After mutated the original bank")
	}
	if len(after.Questions) != 4 || len(after.Classifications[0].Categories) != 4 {
		t.Errorf("after: %d questions, %d primary categories", len(after.Questions), len(after.Classifications[0].Categories))
	}
}
