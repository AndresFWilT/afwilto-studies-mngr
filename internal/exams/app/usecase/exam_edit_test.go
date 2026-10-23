package usecase

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
)

// editEnv is an exam whose bank has four Dominio categories (primary) and
// three Servicio ones, two of the questions carrying several Servicio values:
//
//	e1 Seguridad + S3 · e2 Seguridad + Storage + S3 · e3 Costos + Storage
//	e4 Rendimiento + VPC · e5 Resiliencia + VPC + S3
//
// Inferred Dominio weights: 40 / 20 / 20 / 20.
type editEnv struct {
	bankEnv
	merge in.MergeCategories
}

func editFile() *domain.QuestionFile {
	return fileOf(
		fq("e1", cn("Dominio", "Seguridad"), cn("Servicio", "Amazon S3")),
		fq("e2", cn("Dominio", "Seguridad"), cn("Servicio", "Storage", "Amazon S3")),
		fq("e3", cn("Dominio", "Costos"), cn("Servicio", "Storage")),
		fq("e4", cn("Dominio", "Rendimiento"), cn("Servicio", "VPC")),
		fq("e5", cn("Dominio", "Resiliencia"), cn("Servicio", "VPC", "Amazon S3")),
	)
}

func newEditEnvWith(t *testing.T, file *domain.QuestionFile) editEnv {
	t.Helper()
	e := newImportEnv(t)
	if r := e.run(t, false, "", file); !r.Applied {
		t.Fatalf("seeding: %+v", r.Errors)
	}
	return editEnv{
		bankEnv: bankEnv{
			importEnv: e,
			list:      NewListExamQuestionsService(e.bank),
			one:       NewGetQuestionService(e.bank),
		},
		merge: NewMergeCategoriesService(e.repo, e.bank, &testsupport.StepClock{N: 800}),
	}
}

func newEditEnv(t *testing.T) editEnv { return newEditEnvWith(t, editFile()) }

func (e editEnv) exam(t *testing.T) domain.ExamSummary {
	t.Helper()
	s, err := e.get.Execute(ctx, e.examID)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (e editEnv) classification(t *testing.T, name string) domain.Classification {
	t.Helper()
	for _, c := range e.exam(t).Classifications {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no classification %q in %+v", name, e.exam(t).Classifications)
	return domain.Classification{}
}

func (e editEnv) category(t *testing.T, classification, name string) domain.Category {
	t.Helper()
	for _, k := range e.classification(t, classification).Categories {
		if k.Name == name {
			return k
		}
	}
	t.Fatalf("no category %s/%s", classification, name)
	return domain.Category{}
}

// patch sends the structure edit (and optional exam fields) at the version the exam has now.
func (e editEnv) patch(t *testing.T, edit domain.ExamEdit, st domain.StructureEdit) (domain.ExamSummary, error) {
	t.Helper()
	return e.update.Execute(ctx, e.examID, in.UpdateExamCommand{Version: e.exam(t).Version, Edit: edit, Structure: st})
}

func manualEdit(t *testing.T, e bankEnv, weights map[string]int) domain.StructureEdit {
	t.Helper()
	st := domain.StructureEdit{SetWeights: true}
	for name, percent := range weights {
		st.Weights = append(st.Weights, domain.WeightInput{CategoryID: e.categoryID(t, "Dominio", name), Percent: percent})
	}
	return st
}

func (e editEnv) weights(t *testing.T) map[string]int {
	t.Helper()
	out := map[string]int{}
	for _, c := range e.exam(t).Classifications {
		if c.IsPrimary() {
			for _, k := range c.Categories {
				out[k.Name] = k.WeightPercent
			}
		}
	}
	return out
}

var manual4 = map[string]int{"Seguridad": 30, "Costos": 26, "Rendimiento": 24, "Resiliencia": 20}

func TestPatchSavesManualWeightsTogetherWithExamFieldsUnderOneVersion(t *testing.T) { // S04 item 4, BR-C3
	e := newEditEnv(t)

	got, err := e.patch(t, domain.ExamEdit{Name: ptr("SAA-C03 v2")}, manualEdit(t, e.bankEnv, manual4))

	if err != nil {
		t.Fatal(err)
	}
	if got.Name.String() != "SAA-C03 v2" || got.Version != 2 {
		t.Errorf("exam = %q v%d, want the new name at version 2", got.Name, got.Version)
	}
	if mode := e.classification(t, "Dominio").WeightMode; mode != domain.WeightManual {
		t.Errorf("mode = %s, want manual", mode)
	}
	if w := e.weights(t); w["Seguridad"] != 30 || w["Costos"] != 26 || w["Rendimiento"] != 24 || w["Resiliencia"] != 20 {
		t.Errorf("weights = %v", w)
	}
}

func TestPatchWeightsThatSumTo99WriteNothingNotEvenTheOtherFields(t *testing.T) { // S04-3, atomicity
	e := newEditEnv(t)
	bad := map[string]int{"Seguridad": 30, "Costos": 26, "Rendimiento": 24, "Resiliencia": 19}

	_, err := e.patch(t, domain.ExamEdit{Name: ptr("Renamed")}, manualEdit(t, e.bankEnv, bad))

	wantViolation(t, err, "weights", domain.RuleWeightsSum)
	s := e.exam(t)
	if s.Name.String() != "SAA-C03" || s.Version != 1 || e.classification(t, "Dominio").WeightMode != domain.WeightInferred {
		t.Errorf("exam after a rejected edit = %q v%d mode %s", s.Name, s.Version, e.classification(t, "Dominio").WeightMode)
	}
}

func TestPatchWeightsThatSkipAnAliveCategoryNameTheMissingId(t *testing.T) { // S04-3
	e := newEditEnv(t)
	skip := map[string]int{"Seguridad": 40, "Costos": 30, "Rendimiento": 30}

	_, err := e.patch(t, domain.ExamEdit{}, manualEdit(t, e.bankEnv, skip))

	var v *domain.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v", err)
	}
	missing := e.category(t, "Dominio", "Resiliencia").ID
	for _, violation := range v.Violations {
		if violation.Field == "weights" && violation.Rule == domain.RuleWeightsSum && strings.Contains(violation.Message, missing) {
			return
		}
	}
	t.Errorf("violations = %+v, want weights_sum naming %s", v.Violations, missing)
}

func TestPatchInferredModeReturnsManualWeightsToTheQuestionShares(t *testing.T) { // S04-4
	e := newEditEnv(t)
	if _, err := e.patch(t, domain.ExamEdit{}, manualEdit(t, e.bankEnv, manual4)); err != nil {
		t.Fatal(err)
	}

	got, err := e.patch(t, domain.ExamEdit{}, domain.StructureEdit{InferWeights: true})

	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 3 || e.classification(t, "Dominio").WeightMode != domain.WeightInferred {
		t.Errorf("version %d mode %s", got.Version, e.classification(t, "Dominio").WeightMode)
	}
	if w := e.weights(t); w["Seguridad"] != 40 || w["Costos"] != 20 || w["Rendimiento"] != 20 || w["Resiliencia"] != 20 {
		t.Errorf("weights = %v, want the shares 40/20/20/20", w)
	}
}

func TestPatchPaceNeedsBothValuesOrNeither(t *testing.T) { // S04-5, BR-C4
	e := newEditEnv(t)
	seg := e.category(t, "Dominio", "Seguridad").ID
	pace := func(p domain.PaceEdit) domain.StructureEdit {
		return domain.StructureEdit{Categories: []domain.CategoryEdit{{ID: seg, Pace: p}}}
	}

	_, err := e.patch(t, domain.ExamEdit{}, pace(domain.PaceEdit{QuestionsSet: true, Questions: ptr(10)}))
	wantViolation(t, err, "categories[0].pace", domain.RulePacePair)
	if k := e.category(t, "Dominio", "Seguridad"); k.PaceQuestions != nil || e.exam(t).Version != 1 {
		t.Errorf("a rejected pace was stored: %+v", k)
	}

	if _, err := e.patch(t, domain.ExamEdit{}, pace(domain.PaceEdit{QuestionsSet: true, Questions: ptr(10), MinutesSet: true, Minutes: ptr(20)})); err != nil {
		t.Fatal(err)
	}
	if k := e.category(t, "Dominio", "Seguridad"); k.PaceQuestions == nil || *k.PaceQuestions != 10 || *k.PaceMinutes != 20 {
		t.Errorf("pace = %+v, want 10 questions in 20 minutes", k)
	}

	if _, err := e.patch(t, domain.ExamEdit{}, pace(domain.PaceEdit{QuestionsSet: true, MinutesSet: true})); err != nil {
		t.Fatal(err)
	}
	if k := e.category(t, "Dominio", "Seguridad"); k.PaceQuestions != nil || k.PaceMinutes != nil {
		t.Errorf("pace = %+v, want it cleared by null/null", k)
	}
}

func TestPatchRenamesCategoriesAndClassificationsEverywhere(t *testing.T) {
	e := newEditEnv(t)
	seg := e.category(t, "Dominio", "Seguridad").ID
	svc := e.classification(t, "Servicio").ID

	_, err := e.patch(t, domain.ExamEdit{}, domain.StructureEdit{
		Categories:      []domain.CategoryEdit{{ID: seg, Name: ptr("Seguridad y cumplimiento")}},
		Classifications: []domain.ClassificationRename{{ID: svc, Name: ptr("Servicios")}},
	})

	if err != nil {
		t.Fatal(err)
	}
	k := e.category(t, "Dominio", "Seguridad y cumplimiento")
	if k.ID != seg || k.Key != "seguridad-y-cumplimiento" || e.classification(t, "Servicios").ID != svc {
		t.Errorf("category %+v", k)
	}
	q, _ := e.one.Execute(ctx, e.questionID(t, "e1"))
	want := []domain.CategoryRef{{Classification: "Dominio", Category: "Seguridad y cumplimiento"}, {Classification: "Servicios", Category: "Amazon S3"}}
	if !slices.Equal(q.Categories, want) {
		t.Errorf("question categories = %v, want %v", q.Categories, want)
	}
}

func TestPatchRenameToAnExistingNameIgnoringAccentsIsADuplicate(t *testing.T) { // S04-9
	e := newEditEnvWith(t, fileOf(
		fq("a", cn("Dominio", "Señales")),
		fq("b", cn("Dominio", "Otra")),
	))
	other := e.category(t, "Dominio", "Otra").ID

	_, err := e.patch(t, domain.ExamEdit{}, domain.StructureEdit{Categories: []domain.CategoryEdit{{ID: other, Name: ptr("Senales")}}})

	wantConflict(t, err, domain.RuleDuplicateName)
	if e.category(t, "Dominio", "Otra").ID != other || e.exam(t).Version != 1 {
		t.Error("a rejected rename changed the exam")
	}
}

func TestPatchClassificationRenameToAnotherClassificationIsADuplicate(t *testing.T) { // item 6
	e := newEditEnv(t)

	_, err := e.patch(t, domain.ExamEdit{}, domain.StructureEdit{Classifications: []domain.ClassificationRename{{ID: e.classification(t, "Servicio").ID, Name: ptr("dominio")}}})

	wantConflict(t, err, domain.RuleDuplicateName)
}

func TestPatchWithAStaleVersionIsAConflictAndWritesNothing(t *testing.T) {
	e := newEditEnv(t)

	_, err := e.update.Execute(ctx, e.examID, in.UpdateExamCommand{Version: 7, Structure: domain.StructureEdit{InferWeights: true}})

	wantConflict(t, err, domain.RuleStaleVersion)
	if e.exam(t).Version != 1 {
		t.Error("version moved")
	}
}

func TestPatchWithNothingToChangeIsStillRejected(t *testing.T) {
	e := newEditEnv(t)

	for name, cmd := range map[string]in.UpdateExamCommand{
		"no fields":                  {Version: 1},
		"only empty structure lists": {Version: 1, Structure: domain.StructureEdit{Categories: []domain.CategoryEdit{}}},
	} {
		_, err := e.update.Execute(ctx, e.examID, cmd)
		if err == nil {
			t.Errorf("%s: accepted", name)
			continue
		}
		wantViolation(t, err, "body", "required")
	}
}

func TestPatchWithOnlyStructureIsAccepted(t *testing.T) {
	e := newEditEnv(t)

	got, err := e.patch(t, domain.ExamEdit{}, domain.StructureEdit{InferWeights: true})

	if err != nil || got.Version != 2 {
		t.Errorf("version %d, err %v, want an accepted structure-only edit", got.Version, err)
	}
}

func TestPatchExamNameClashWritesNeitherTheNameNorTheStructure(t *testing.T) {
	e := newEditEnv(t)
	e.add(t, subjectA, "Other exam")
	seg := e.category(t, "Dominio", "Seguridad").ID

	_, err := e.patch(t, domain.ExamEdit{Name: ptr("other EXAM")}, domain.StructureEdit{Categories: []domain.CategoryEdit{{ID: seg, Name: ptr("Renamed")}}})

	wantConflict(t, err, domain.RuleDuplicateName)
	if e.category(t, "Dominio", "Seguridad").ID != seg {
		t.Error("the category was renamed although the exam name clashed")
	}
}

// ── swapping the primary ───────────────────────────────────────────────────

func TestPatchPrimarySwapIsRefusedListingTheQuestionsWithTwoCategories(t *testing.T) { // S04-8, BR-C2
	e := newEditEnv(t)

	_, err := e.patch(t, domain.ExamEdit{}, domain.StructureEdit{PrimaryID: e.classification(t, "Servicio").ID})

	var v *domain.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v", err)
	}
	for _, violation := range v.Violations {
		if violation.Field == "primaryClassificationId" && violation.Rule == domain.RulePrimarySingleValued &&
			strings.Contains(violation.Message, "e2") && strings.Contains(violation.Message, "e5") {
			if !e.classification(t, "Dominio").IsPrimary() {
				t.Error("a refused swap moved the primary")
			}
			return
		}
	}
	t.Errorf("violations = %+v, want primary_single_valued listing e2 and e5", v.Violations)
}

func TestPatchPrimarySwapMovesThePrimaryAndInfersTheNewWeights(t *testing.T) { // item 4
	e := newEditEnvWith(t, fileOf(
		fq("a", cn("Dominio", "X"), cn("Servicio", "P")),
		fq("b", cn("Dominio", "X"), cn("Servicio", "Q")),
		fq("c", cn("Dominio", "Y"), cn("Servicio", "P")),
	))
	if _, err := e.patch(t, domain.ExamEdit{}, domain.StructureEdit{SetWeights: true, Weights: []domain.WeightInput{
		{CategoryID: e.category(t, "Dominio", "X").ID, Percent: 80}, {CategoryID: e.category(t, "Dominio", "Y").ID, Percent: 20}}}); err != nil {
		t.Fatal(err)
	}

	_, err := e.patch(t, domain.ExamEdit{}, domain.StructureEdit{PrimaryID: e.classification(t, "Servicio").ID})

	if err != nil {
		t.Fatal(err)
	}
	svc, dom := e.classification(t, "Servicio"), e.classification(t, "Dominio")
	if !svc.IsPrimary() || dom.IsPrimary() || svc.WeightMode != domain.WeightInferred {
		t.Fatalf("servicio %+v, dominio %+v", svc, dom)
	}
	if p, q := e.category(t, "Servicio", "P").WeightPercent, e.category(t, "Servicio", "Q").WeightPercent; p != 67 || q != 33 {
		t.Errorf("weights P=%d Q=%d, want the shares 67/33", p, q)
	}
}

// ── merge ──────────────────────────────────────────────────────────────────

func (e editEnv) mergeNames(t *testing.T, classification, from, into string) (domain.ExamSummary, error) {
	t.Helper()
	return e.merge.Execute(ctx, e.examID, in.MergeCategoriesCommand{From: e.category(t, classification, from).ID, Into: e.category(t, classification, into).ID})
}

func TestMergeMovesTheLinksKeepsOneWhenAQuestionHadBothAndDeletesFrom(t *testing.T) { // S04-6
	e := newEditEnv(t)

	got, err := e.mergeNames(t, "Servicio", "Storage", "Amazon S3")

	if err != nil {
		t.Fatal(err)
	}
	if got.Version != 2 {
		t.Errorf("version = %d, want 2", got.Version)
	}
	for _, k := range e.classification(t, "Servicio").Categories {
		if k.Name == "Storage" {
			t.Error("Storage is still in the exam")
		}
	}
	q2, _ := e.one.Execute(ctx, e.questionID(t, "e2")) // had both
	if s3 := countRefs(q2, "Servicio", "Amazon S3"); s3 != 1 || countRefs(q2, "Servicio", "Storage") != 0 {
		t.Errorf("e2 categories = %v, want one link to Amazon S3", q2.Categories)
	}
	q3, _ := e.one.Execute(ctx, e.questionID(t, "e3")) // had Storage only
	if countRefs(q3, "Servicio", "Amazon S3") != 1 {
		t.Errorf("e3 categories = %v, want its link moved to Amazon S3", q3.Categories)
	}
	if n := e.category(t, "Servicio", "Amazon S3").QuestionCount; n != 4 { // e1 e2 e3 e5
		t.Errorf("Amazon S3 has %d questions, want 4", n)
	}
}

func countRefs(q domain.Question, classification, category string) int {
	n := 0
	for _, r := range q.Categories {
		if r.Classification == classification && r.Category == category {
			n++
		}
	}
	return n
}

func TestMergeAddsFromsManualWeightToInto(t *testing.T) { // S04-6
	e := newEditEnv(t)
	if _, err := e.patch(t, domain.ExamEdit{}, manualEdit(t, e.bankEnv, map[string]int{"Seguridad": 10, "Costos": 20, "Rendimiento": 30, "Resiliencia": 40})); err != nil {
		t.Fatal(err)
	}

	if _, err := e.mergeNames(t, "Dominio", "Seguridad", "Costos"); err != nil {
		t.Fatal(err)
	}

	w := e.weights(t)
	if w["Costos"] != 30 || w["Rendimiento"] != 30 || w["Resiliencia"] != 40 || len(w) != 3 {
		t.Errorf("weights = %v, want the 10 and 20 added up to 30 and a total of 100", w)
	}
}

func TestMergeRecomputesInferredWeights(t *testing.T) {
	e := newEditEnv(t)

	if _, err := e.mergeNames(t, "Dominio", "Costos", "Seguridad"); err != nil { // 2 + 1 of 5
		t.Fatal(err)
	}

	w := e.weights(t)
	if w["Seguridad"] != 60 || w["Rendimiento"] != 20 || w["Resiliencia"] != 20 {
		t.Errorf("weights = %v, want 60/20/20", w)
	}
}

func TestMergeScope(t *testing.T) { // S04-7, BR-C5
	e := newEditEnv(t)
	seg, s3 := e.category(t, "Dominio", "Seguridad").ID, e.category(t, "Servicio", "Amazon S3").ID
	storage := e.category(t, "Servicio", "Storage").ID
	if _, err := e.merge.Execute(ctx, e.examID, in.MergeCategoriesCommand{From: storage, Into: s3}); err != nil {
		t.Fatal(err)
	}

	cases := map[string]in.MergeCategoriesCommand{
		"different classifications": {From: seg, Into: s3},
		"a deleted category":        {From: storage, Into: s3},
		"into itself":               {From: seg, Into: seg},
	}
	for name, cmd := range cases {
		_, err := e.merge.Execute(ctx, e.examID, cmd)
		var v *domain.ValidationError
		if !errors.As(err, &v) || v.Violations[0].Rule != domain.RuleMergeScope {
			t.Errorf("%s: err = %v, want merge_scope", name, err)
		}
	}
	if e.exam(t).Version != 2 {
		t.Errorf("version = %d, only the first merge may have counted", e.exam(t).Version)
	}
}

func TestMergeInAnUnknownExamIsNotFound(t *testing.T) {
	e := newEditEnv(t)

	_, err := e.merge.Execute(ctx, "0abcdef0-0000-7000-8000-0000000dead0", in.MergeCategoriesCommand{From: "a", Into: "b"})

	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
