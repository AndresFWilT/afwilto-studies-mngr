package domain

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"
)

var editNow = time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)

const examVersion = 4

func bkExam() Exam {
	return Exam{
		ID: "exam-1", SubjectID: "subject-1", Name: RehydrateExamName("SAA-C03"), QuestionCount: 65, TimeLimitMin: 130, PassPercent: 72,
		Version: examVersion, CreatedAt: editNow.Add(-time.Hour), UpdatedAt: editNow.Add(-time.Hour),
	}
}

// bkBank: Dominio (primary, inferred 40/20/40/0) with 5 classified questions and Servicio (secondary).
//
//	q1 Seguridad + VPC · q2 Seguridad + Amazon S3 · q3 Costos + Storage
//	q4 Rendimiento + Amazon S3 · q5 Rendimiento · q6 VPC only (no Dominio category)
func bkBank() Bank {
	return Bank{
		Classifications: []Classification{
			bkClass("cl-dom", "Dominio", 0, WeightInferred,
				bkCat("k-seg", "Seguridad", 40), bkCat("k-cos", "Costos", 20), bkCat("k-ren", "Rendimiento", 40), bkCat("k-res", "Resiliencia", 0)),
			bkClass("cl-svc", "Servicio", 1, WeightInferred,
				bkCat("k-s3", "Amazon S3", 0), bkCat("k-sto", "Storage", 0), bkCat("k-vpc", "VPC", 0)),
		},
		Questions: []Question{
			bkQ("q1", ref2("Dominio", "Seguridad"), ref2("Servicio", "VPC")),
			bkQ("q2", ref2("Dominio", "Seguridad"), ref2("Servicio", "Amazon S3")),
			bkQ("q3", ref2("Dominio", "Costos"), ref2("Servicio", "Storage")),
			bkQ("q4", ref2("Dominio", "Rendimiento"), ref2("Servicio", "Amazon S3")),
			bkQ("q5", ref2("Dominio", "Rendimiento")),
			bkQ("q6", ref2("Servicio", "VPC")),
		},
	}
}

// bkManualBank is bkBank with manual weights 30/26/24/20 on Dominio.
func bkManualBank() Bank {
	b := bkBank()
	b.Classifications[0].WeightMode = WeightManual
	for i, w := range []int{30, 26, 24, 20} {
		b.Classifications[0].Categories[i].WeightPercent = w
	}
	return b
}

func planEdit(bank Bank, edit ExamEdit, st StructureEdit) (EditWrite, error) {
	return PlanExamEdit(bkExam(), bank, examVersion, edit, st, editNow)
}

func mustPlanEdit(t *testing.T, bank Bank, edit ExamEdit, st StructureEdit) EditWrite {
	t.Helper()
	w, err := planEdit(bank, edit, st)
	if err != nil {
		t.Fatalf("PlanExamEdit: %v", err)
	}
	return w
}

func mustViolate(t *testing.T, err error, field, rule string) Violation {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v, want a validation error %s/%s", err, field, rule)
	}
	for _, got := range v.Violations {
		if got.Field == field && got.Rule == rule {
			return got
		}
	}
	t.Fatalf("violations = %+v, want %s/%s", v.Violations, field, rule)
	return Violation{}
}

func mustConflict(t *testing.T, err error, rule string) *ConflictError {
	t.Helper()
	var c *ConflictError
	if !errors.As(err, &c) || c.Rule != rule {
		t.Fatalf("err = %v, want a %s conflict", err, rule)
	}
	return c
}

func changedCat(w EditWrite, id string) (Category, bool) {
	for _, c := range w.Categories {
		if c.ID == id {
			return c, true
		}
	}
	return Category{}, false
}

func changedClass(w EditWrite, id string) (Classification, bool) {
	for _, c := range w.Classifications {
		if c.ID == id {
			return c, true
		}
	}
	return Classification{}, false
}

func manualWeights(pairs ...any) StructureEdit {
	st := StructureEdit{SetWeights: true}
	for i := 0; i < len(pairs); i += 2 {
		st.Weights = append(st.Weights, WeightInput{CategoryID: pairs[i].(string), Percent: pairs[i+1].(int)})
	}
	return st
}

// ── the exam fields and the version ────────────────────────────────────────

func TestPlanExamEditAppliesTheExamFieldsAndIncrementsTheVersion(t *testing.T) {
	w := mustPlanEdit(t, bkBank(), ExamEdit{Name: ptr("SAA-C04"), PassPercent: ptr(70)}, StructureEdit{})

	if w.Exam == nil || w.Exam.Name.String() != "SAA-C04" || w.Exam.PassPercent != 70 || w.Exam.QuestionCount != 65 ||
		w.Exam.Version != examVersion+1 || !w.Exam.UpdatedAt.Equal(editNow) {
		t.Errorf("exam = %+v", w.Exam)
	}
	if w.ExpectedVersion != examVersion || w.ExamID != "exam-1" {
		t.Errorf("expected version %d, exam id %q", w.ExpectedVersion, w.ExamID)
	}
	if len(w.Classifications)+len(w.Categories)+len(w.Weights) != 0 || w.Merge != nil {
		t.Errorf("a name-only edit wrote structure: %+v", w)
	}
}

func TestPlanExamEditWithAStaleVersionIsAConflict(t *testing.T) { // BR optimistic concurrency
	_, err := PlanExamEdit(bkExam(), bkBank(), examVersion-1, ExamEdit{}, StructureEdit{Categories: []CategoryEdit{{ID: "k-seg", Name: ptr("Otra")}}}, editNow)

	mustConflict(t, err, RuleStaleVersion)
}

func TestPlanExamEditReportsExamAndStructureViolationsTogether(t *testing.T) {
	_, err := planEdit(bkBank(), ExamEdit{QuestionCount: ptr(0)},
		StructureEdit{Categories: []CategoryEdit{{ID: "k-seg", Pace: PaceEdit{QuestionsSet: true, Questions: ptr(10)}}}})

	mustViolate(t, err, "questionCount", RuleRange)
	mustViolate(t, err, "categories[0].pace", RulePacePair)
}

// ── weights ────────────────────────────────────────────────────────────────

func TestPlanExamEditSavesManualWeightsAndSwitchesToManual(t *testing.T) { // BR-C3
	w := mustPlanEdit(t, bkBank(), ExamEdit{}, manualWeights("k-seg", 30, "k-cos", 26, "k-ren", 24, "k-res", 20))

	got := weightsOf(w.Weights)
	if got["k-seg"] != 30 || got["k-cos"] != 26 || got["k-ren"] != 24 || got["k-res"] != 20 || len(got) != 4 {
		t.Errorf("weights = %v", got)
	}
	if c, ok := changedClass(w, "cl-dom"); !ok || c.WeightMode != WeightManual || c.Position != PositionPrimary {
		t.Errorf("cl-dom = %+v (changed %v), want manual primary", c, ok)
	}
}

func TestPlanExamEditRejectsWeightsThatDoNotSumTo100(t *testing.T) { // S04-3
	_, err := planEdit(bkBank(), ExamEdit{}, manualWeights("k-seg", 30, "k-cos", 26, "k-ren", 24, "k-res", 19))

	v := mustViolate(t, err, "weights", RuleWeightsSum)
	if !strings.Contains(v.Message, "99") {
		t.Errorf("message %q should state the sum 99", v.Message)
	}
}

func TestPlanExamEditRejectsWeightsThatSkipAnAliveCategory(t *testing.T) { // S04-3
	_, err := planEdit(bkBank(), ExamEdit{}, manualWeights("k-seg", 40, "k-cos", 30, "k-ren", 30))

	v := mustViolate(t, err, "weights", RuleWeightsSum)
	if !strings.Contains(v.Message, "k-res") {
		t.Errorf("message %q should name the missing category id k-res", v.Message)
	}
}

func TestPlanExamEditRejectsWeightsOnAnExamWithoutAPrimary(t *testing.T) {
	bank := Bank{Classifications: []Classification{bkClass("cl-svc", "Servicio", 1, WeightInferred, bkCat("k-s3", "Amazon S3", 0))}}

	_, err := planEdit(bank, ExamEdit{}, manualWeights("k-s3", 100))

	mustViolate(t, err, "weights", RuleWeightsSum)
}

func TestPlanExamEditInferredModeReturnsTheWeightsToTheQuestionShares(t *testing.T) { // S04-4
	w := mustPlanEdit(t, bkManualBank(), ExamEdit{}, StructureEdit{InferWeights: true})

	got := weightsOf(w.Weights)
	if got["k-seg"] != 40 || got["k-cos"] != 20 || got["k-ren"] != 40 || got["k-res"] != 0 || len(got) != 4 {
		t.Errorf("weights = %v, want the question shares 40/20/40/0", got)
	}
	if c, ok := changedClass(w, "cl-dom"); !ok || c.WeightMode != WeightInferred {
		t.Errorf("cl-dom = %+v (changed %v), want inferred", c, ok)
	}
}

func TestPlanExamEditRejectsManualWeightsTogetherWithInferredMode(t *testing.T) {
	st := manualWeights("k-seg", 100, "k-cos", 0, "k-ren", 0, "k-res", 0)
	st.InferWeights = true

	_, err := planEdit(bkBank(), ExamEdit{}, st)

	mustViolate(t, err, "weightMode", "format")
}

// ── pace ───────────────────────────────────────────────────────────────────

func TestPlanExamEditPacePair(t *testing.T) { // S04-5, BR-C4
	pace := func(qSet bool, q *int, mSet bool, m *int) StructureEdit {
		return StructureEdit{Categories: []CategoryEdit{{ID: "k-seg", Pace: PaceEdit{QuestionsSet: qSet, Questions: q, MinutesSet: mSet, Minutes: m}}}}
	}
	cases := []struct {
		name      string
		st        StructureEdit
		wantRule  string // "" means accepted
		wantPace  [2]*int
		wantTouch bool // the category is in the write
	}{
		{"both values set", pace(true, ptr(10), true, ptr(20)), "", [2]*int{ptr(10), ptr(20)}, true},
		{"S04-5: questions without minutes", pace(true, ptr(10), false, nil), RulePacePair, [2]*int{}, false},
		{"minutes without questions", pace(false, nil, true, ptr(20)), RulePacePair, [2]*int{}, false},
		{"one value and one null", pace(true, ptr(10), true, nil), RulePacePair, [2]*int{}, false},
		{"both null clears the pair", pace(true, nil, true, nil), "", [2]*int{nil, nil}, true},
		{"neither sent leaves the pair", pace(false, nil, false, nil), "", [2]*int{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bank := bkBank()
			bank.Classifications[0].Categories[0].PaceQuestions, bank.Classifications[0].Categories[0].PaceMinutes = ptr(5), ptr(5)

			w, err := planEdit(bank, ExamEdit{}, tc.st)

			if tc.wantRule != "" {
				mustViolate(t, err, "categories[0].pace", tc.wantRule)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			c, touched := changedCat(w, "k-seg")
			if touched != tc.wantTouch {
				t.Fatalf("category in the write = %v, want %v", touched, tc.wantTouch)
			}
			if touched && (!equalPtr(c.PaceQuestions, tc.wantPace[0]) || !equalPtr(c.PaceMinutes, tc.wantPace[1])) {
				t.Errorf("pace = %v/%v, want %v/%v", deref2(c.PaceQuestions), deref2(c.PaceMinutes), deref2(tc.wantPace[0]), deref2(tc.wantPace[1]))
			}
		})
	}
}

func TestPlanExamEditPaceIsRangeCheckedFrom1To10000(t *testing.T) { // phase-2 fix 3
	pace := func(q, m int) StructureEdit {
		return StructureEdit{Categories: []CategoryEdit{{ID: "k-seg", Pace: PaceEdit{QuestionsSet: true, Questions: ptr(q), MinutesSet: true, Minutes: ptr(m)}}}}
	}
	cases := []struct {
		name  string
		st    StructureEdit
		field string // "" means accepted
	}{
		{"1 and 1 are the lower edge", pace(1, 1), ""},
		{"10,000 and 10,000 are the upper edge", pace(10_000, 10_000), ""},
		{"zero questions", pace(0, 20), "categories[0].paceQuestions"},
		{"zero minutes", pace(10, 0), "categories[0].paceMinutes"},
		{"negative questions", pace(-5, 20), "categories[0].paceQuestions"},
		{"10,001 questions", pace(10_001, 20), "categories[0].paceQuestions"},
		{"10,001 minutes", pace(10, 10_001), "categories[0].paceMinutes"},
		{"3,000,000,000 questions", pace(3_000_000_000, 20), "categories[0].paceQuestions"},
		{"MaxInt64 minutes", pace(10, math.MaxInt64), "categories[0].paceMinutes"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := planEdit(bkBank(), ExamEdit{}, tc.st)

			if tc.field == "" {
				if err != nil {
					t.Fatalf("err = %v, want accepted", err)
				}
				return
			}
			mustViolate(t, err, tc.field, RuleRange)
		})
	}
}

func equalPtr(a, b *int) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func deref2(p *int) string {
	if p == nil {
		return "null"
	}
	return fmt.Sprint(*p)
}

// ── renames ────────────────────────────────────────────────────────────────

func TestPlanExamEditRenamesACategoryAndItsKey(t *testing.T) {
	w := mustPlanEdit(t, bkBank(), ExamEdit{}, StructureEdit{Categories: []CategoryEdit{{ID: "k-seg", Name: ptr("  Seguridad y cumplimiento ")}}})

	c, ok := changedCat(w, "k-seg")
	if !ok || c.Name != "Seguridad y cumplimiento" || c.Key != "seguridad-y-cumplimiento" || c.Fold != "seguridad y cumplimiento" {
		t.Errorf("category = %+v (changed %v)", c, ok)
	}
}

func TestPlanExamEditRenamesAClassification(t *testing.T) {
	w := mustPlanEdit(t, bkBank(), ExamEdit{}, StructureEdit{Classifications: []ClassificationRename{{ID: "cl-svc", Name: ptr("Servicios AWS")}}})

	c, ok := changedClass(w, "cl-svc")
	if !ok || c.Name != "Servicios AWS" || c.Fold != "servicios aws" || c.Position != PositionSecondary || c.WeightMode != WeightInferred {
		t.Errorf("classification = %+v (changed %v)", c, ok)
	}
}

func TestPlanExamEditAcceptsRenamesThatOnlyChangeCaseOrSwapNames(t *testing.T) {
	cases := map[string]StructureEdit{
		"the same name in another case": {Categories: []CategoryEdit{{ID: "k-seg", Name: ptr("SEGURIDAD")}}},
		"two categories swap names": {Categories: []CategoryEdit{
			{ID: "k-s3", Name: ptr("Storage")}, {ID: "k-sto", Name: ptr("Amazon S3")}}},
		"a name that exists in the other classification": {Categories: []CategoryEdit{{ID: "k-vpc", Name: ptr("Seguridad")}}},
		"a classification in another case":               {Classifications: []ClassificationRename{{ID: "cl-dom", Name: ptr("DOMINIO")}}},
	}
	for name, st := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := planEdit(bkBank(), ExamEdit{}, st); err != nil {
				t.Errorf("err = %v, want accepted", err)
			}
		})
	}
}

func TestPlanExamEditRejectsARenameToAnExistingNameInTheSameScope(t *testing.T) { // item 6, S04-9
	withSenales := func() Bank {
		b := bkBank()
		b.Classifications[0].Categories = append(b.Classifications[0].Categories, bkCat("k-sen", "Señales", 0))
		return b
	}
	cases := []struct {
		name  string
		bank  Bank
		st    StructureEdit
		field string
	}{
		{"S04-9: accents only (Senales vs Señales)", withSenales(), StructureEdit{Categories: []CategoryEdit{{ID: "k-cos", Name: ptr("Senales")}}}, "categories[0].name"},
		{"case only", bkBank(), StructureEdit{Categories: []CategoryEdit{{ID: "k-cos", Name: ptr("seguridad")}}}, "categories[0].name"},
		{"surrounding and inner spaces", bkBank(), StructureEdit{Categories: []CategoryEdit{{ID: "k-vpc", Name: ptr(" amazon   s3 ")}}}, "categories[0].name"},
		{"two categories renamed to the same name", bkBank(), StructureEdit{Categories: []CategoryEdit{{ID: "k-s3", Name: ptr("Nuevo")}, {ID: "k-sto", Name: ptr("nuevo")}}}, "categories[1].name"},
		{"a classification", bkBank(), StructureEdit{Classifications: []ClassificationRename{{ID: "cl-svc", Name: ptr("dominio")}}}, "classifications[0].name"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := planEdit(tc.bank, ExamEdit{}, tc.st)

			c := mustConflict(t, err, RuleDuplicateName)
			if c.Field != tc.field {
				t.Errorf("field = %q, want %q", c.Field, tc.field)
			}
		})
	}
}

func TestPlanExamEditValidatesNamesAndIds(t *testing.T) {
	long := func(n int) *string { return ptr(strings.Repeat("x", n)) }
	cases := []struct {
		name        string
		st          StructureEdit
		field, rule string
	}{
		{"empty category name", StructureEdit{Categories: []CategoryEdit{{ID: "k-seg", Name: ptr("  ")}}}, "categories[0].name", "required"},
		{"category name of 81", StructureEdit{Categories: []CategoryEdit{{ID: "k-seg", Name: long(81)}}}, "categories[0].name", "max_length"},
		{"classification name of 41", StructureEdit{Classifications: []ClassificationRename{{ID: "cl-svc", Name: long(41)}}}, "classifications[0].name", "max_length"},
		{"empty classification name", StructureEdit{Classifications: []ClassificationRename{{ID: "cl-svc", Name: ptr("")}}}, "classifications[0].name", "required"},
		{"unknown category", StructureEdit{Categories: []CategoryEdit{{ID: "k-zzz", Name: ptr("X")}}}, "categories[0].id", RuleNotFound},
		{"unknown classification", StructureEdit{Classifications: []ClassificationRename{{ID: "cl-zzz", Name: ptr("X")}}}, "classifications[0].id", RuleNotFound},
		{"unknown primary", StructureEdit{PrimaryID: "cl-zzz"}, "primaryClassificationId", RuleNotFound},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := planEdit(bkBank(), ExamEdit{}, tc.st)

			mustViolate(t, err, tc.field, tc.rule)
		})
	}
}

// ── swapping the primary ───────────────────────────────────────────────────

func TestPlanExamEditSwapsThePrimaryAndResetsTheWeightsToInferred(t *testing.T) { // item 4
	w := mustPlanEdit(t, bkManualBank(), ExamEdit{}, StructureEdit{PrimaryID: "cl-svc"})

	svc, okS := changedClass(w, "cl-svc")
	dom, okD := changedClass(w, "cl-dom")
	if !okS || !okD || svc.Position != PositionPrimary || svc.WeightMode != WeightInferred || dom.Position != PositionSecondary || dom.WeightMode != WeightInferred {
		t.Fatalf("svc %+v (%v), dom %+v (%v)", svc, okS, dom, okD)
	}
	got := weightsOf(w.Weights)
	want := map[string]int{"k-s3": 40, "k-sto": 20, "k-vpc": 40, "k-seg": 0, "k-cos": 0, "k-ren": 0, "k-res": 0}
	for id, p := range want {
		if g, ok := got[id]; !ok || g != p {
			t.Errorf("weight of %s = %d (present %v), want %d; all %v", id, g, ok, p, got)
		}
	}
}

func TestPlanExamEditSwapDemotesBeforePromotingSoThePrimaryIsNeverTwo(t *testing.T) {
	bank := bkBank()
	slices.Reverse(bank.Classifications) // the classification to promote comes first in creation order

	w := mustPlanEdit(t, bank, ExamEdit{}, StructureEdit{PrimaryID: "cl-svc"})

	if len(w.Classifications) != 2 || w.Classifications[0].Position != PositionSecondary || w.Classifications[1].Position != PositionPrimary {
		t.Errorf("classification order = %+v, want the demotion first", w.Classifications)
	}
}

func TestPlanExamEditRefusesToSwapWhenAQuestionHasTwoCategoriesInTheNewPrimary(t *testing.T) { // S04-8, BR-C2
	bank := bkBank()
	bank.Questions = append(bank.Questions, bkQ("both", ref2("Servicio", "Amazon S3"), ref2("Servicio", "Storage")))

	_, err := planEdit(bank, ExamEdit{}, StructureEdit{PrimaryID: "cl-svc"})

	v := mustViolate(t, err, "primaryClassificationId", RulePrimarySingleValued)
	if !strings.Contains(v.Message, "both") {
		t.Errorf("message %q should list the key of the offending question", v.Message)
	}
}

func TestPlanExamEditListsAtMost10OffendingQuestionKeys(t *testing.T) { // S04-8
	bank := bkBank()
	for i := 1; i <= 12; i++ {
		bank.Questions = append(bank.Questions, bkQ(fmt.Sprintf("dup-%02d", i), ref2("Servicio", "Amazon S3"), ref2("Servicio", "Storage")))
	}

	_, err := planEdit(bank, ExamEdit{}, StructureEdit{PrimaryID: "cl-svc"})

	v := mustViolate(t, err, "primaryClassificationId", RulePrimarySingleValued)
	listed := 0
	for i := 1; i <= 12; i++ {
		if strings.Contains(v.Message, fmt.Sprintf("dup-%02d", i)) {
			listed++
		}
	}
	if listed != 10 || !strings.Contains(v.Message, "dup-01") || !strings.Contains(v.Message, "dup-10") || strings.Contains(v.Message, "dup-11") || !strings.Contains(v.Message, "and 2 more") {
		t.Errorf("message %q lists %d keys, want the first 10", v.Message, listed)
	}
}

func TestPlanExamEditNamingTheCurrentPrimaryChangesNothing(t *testing.T) {
	w := mustPlanEdit(t, bkManualBank(), ExamEdit{}, StructureEdit{PrimaryID: "cl-dom"})

	if len(w.Classifications)+len(w.Categories)+len(w.Weights) != 0 {
		t.Errorf("write = %+v, want no structure change", w)
	}
}

func TestPlanExamEditPromotesTheOnlyClassificationOfAnExamWithoutPrimary(t *testing.T) { // FR-3.1 rule 4 follow-up
	bank := Bank{
		Classifications: []Classification{bkClass("cl-svc", "Servicio", 1, WeightInferred, bkCat("k-s3", "Amazon S3", 0), bkCat("k-vpc", "VPC", 0))},
		Questions:       []Question{bkQ("a", ref2("Servicio", "Amazon S3")), bkQ("b", ref2("Servicio", "VPC")), bkQ("c", ref2("Servicio", "VPC"))},
	}

	w := mustPlanEdit(t, bank, ExamEdit{}, StructureEdit{PrimaryID: "cl-svc"})

	c, ok := changedClass(w, "cl-svc")
	if !ok || c.Position != PositionPrimary || c.WeightMode != WeightInferred {
		t.Fatalf("classification = %+v (changed %v)", c, ok)
	}
	if got := weightsOf(w.Weights); got["k-s3"] != 33 || got["k-vpc"] != 67 {
		t.Errorf("weights = %v, want 33/67", got)
	}
}

func TestPlanExamEditManualWeightsInTheSameRequestCoverTheNewPrimary(t *testing.T) {
	w := mustPlanEdit(t, bkBank(), ExamEdit{}, StructureEdit{PrimaryID: "cl-svc", SetWeights: true,
		Weights: []WeightInput{{"k-s3", 50}, {"k-sto", 30}, {"k-vpc", 20}}})

	svc, _ := changedClass(w, "cl-svc")
	got := weightsOf(w.Weights)
	if svc.Position != PositionPrimary || svc.WeightMode != WeightManual || got["k-s3"] != 50 || got["k-sto"] != 30 || got["k-vpc"] != 20 {
		t.Errorf("svc %+v, weights %v", svc, got)
	}

	_, err := planEdit(bkBank(), ExamEdit{}, StructureEdit{PrimaryID: "cl-svc", SetWeights: true,
		Weights: []WeightInput{{"k-seg", 100}}})
	mustViolate(t, err, "weights[0].categoryId", RuleNotFound) // the old primary's categories no longer count
}

// ── merge ──────────────────────────────────────────────────────────────────

func mustPlanMerge(t *testing.T, bank Bank, from, into string) EditWrite {
	t.Helper()
	w, err := PlanMerge(bkExam(), bank, from, into, editNow)
	if err != nil {
		t.Fatalf("PlanMerge: %v", err)
	}
	return w
}

func TestPlanMergeMovesTheLinksDeletesFromAndIncrementsTheVersion(t *testing.T) { // S04-6
	w := mustPlanMerge(t, bkBank(), "k-sto", "k-s3")

	if w.Merge == nil || *w.Merge != (CategoryMerge{ClassificationID: "cl-svc", From: "k-sto", Into: "k-s3"}) {
		t.Errorf("merge = %+v", w.Merge)
	}
	if w.Exam == nil || w.Exam.Version != examVersion+1 || w.ExpectedVersion != examVersion || !w.Exam.UpdatedAt.Equal(editNow) {
		t.Errorf("exam = %+v, expected version %d", w.Exam, w.ExpectedVersion)
	}
	if len(w.Weights) != 0 {
		t.Errorf("weights = %v, the secondary classification has none", w.Weights)
	}
}

func TestPlanMergeAddsFromsManualWeightToInto(t *testing.T) { // S04-6
	bank := bkBank()
	bank.Classifications[0].WeightMode = WeightManual
	for i, w := range []int{10, 20, 30, 40} { // Seguridad 10, Costos 20
		bank.Classifications[0].Categories[i].WeightPercent = w
	}

	w := mustPlanMerge(t, bank, "k-seg", "k-cos")

	got := weightsOf(w.Weights)
	if len(got) != 1 || got["k-cos"] != 30 {
		t.Errorf("weights = %v, want only the merged-into category at 10 + 20 = 30", got)
	}
}

func TestPlanMergeRecomputesInferredWeightsOverTheMergedShares(t *testing.T) {
	w := mustPlanMerge(t, bkBank(), "k-cos", "k-seg") // Seguridad 2 + Costos 1 = 3 of 5

	got := weightsOf(w.Weights)
	want := map[string]int{"k-seg": 60, "k-ren": 40, "k-res": 0}
	if len(got) != 3 {
		t.Fatalf("weights = %v, want %v (the merged-away category is gone)", got, want)
	}
	for id, p := range want {
		if got[id] != p {
			t.Errorf("weight of %s = %d, want %d", id, got[id], p)
		}
	}
}

func TestPlanMergeCountsAQuestionOnceWhenItHadBothCategories(t *testing.T) { // S04-6 dedupe in the weights
	bank := bkBank()
	bank.Classifications[0].WeightMode = WeightInferred
	// A legacy state: a question linked to both ends in the primary.
	bank.Questions = append(bank.Questions, bkQ("both", ref2("Dominio", "Seguridad"), ref2("Dominio", "Costos")))

	w := mustPlanMerge(t, bank, "k-cos", "k-seg")

	// Seguridad: q1, q2, q3 (Costos), both = 4 distinct questions; Rendimiento 2; total 6.
	if got := weightsOf(w.Weights); got["k-seg"] != 67 || got["k-ren"] != 33 {
		t.Errorf("weights = %v, want 67/33", got)
	}
}

func TestPlanMergeRefusesWhatIsNotTwoAliveCategoriesOfOneClassification(t *testing.T) { // S04-7, BR-C5
	cases := []struct{ name, from, into, field string }{
		{"categories of different classifications", "k-seg", "k-s3", "into"},
		{"a category into itself", "k-seg", "k-seg", "into"},
		{"a deleted or unknown from", "k-gone", "k-seg", "from"},
		{"a deleted or unknown into", "k-seg", "k-gone", "into"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := PlanMerge(bkExam(), bkBank(), tc.from, tc.into, editNow)

			mustViolate(t, err, tc.field, RuleMergeScope)
		})
	}
}

// ── deleting a question ────────────────────────────────────────────────────

func TestPlanDeleteQuestionRecomputesTheInferredWeights(t *testing.T) { // S04-2
	w, err := PlanDeleteQuestion(bkExam(), bkBank(), "q-q1", editNow)
	if err != nil {
		t.Fatal(err)
	}

	if w.DeleteQuestionID != "q-q1" || w.Exam != nil || !w.Now.Equal(editNow) {
		t.Errorf("write = %+v", w)
	}
	got := weightsOf(w.Weights) // Seguridad 1, Costos 1, Rendimiento 2 of 4
	want := map[string]int{"k-seg": 25, "k-cos": 25, "k-ren": 50, "k-res": 0}
	for id, p := range want {
		if got[id] != p {
			t.Errorf("weight of %s = %d, want %d (all %v)", id, got[id], p, got)
		}
	}
}

func TestPlanDeleteQuestionLeavesManualWeightsAlone(t *testing.T) {
	w, err := PlanDeleteQuestion(bkExam(), bkManualBank(), "q-q1", editNow)
	if err != nil {
		t.Fatal(err)
	}

	if w.DeleteQuestionID != "q-q1" || len(w.Weights) != 0 {
		t.Errorf("write = %+v, want the delete only", w)
	}
}

func TestPlanDeleteQuestionOfTheLastClassifiedOneZeroesTheWeights(t *testing.T) {
	bank := bkBank()
	bank.Questions = []Question{bkQ("only", ref2("Dominio", "Seguridad"))}

	w, err := PlanDeleteQuestion(bkExam(), bank, "q-only", editNow)
	if err != nil {
		t.Fatal(err)
	}

	for _, cw := range w.Weights {
		if cw.Percent != 0 {
			t.Errorf("weights = %v, want all zero", w.Weights)
		}
	}
	if len(w.Weights) != 4 {
		t.Errorf("weights = %v, want one per primary category", w.Weights)
	}
}

func TestPlanDeleteQuestionWithoutAPrimaryWritesNoWeights(t *testing.T) {
	bank := Bank{
		Classifications: []Classification{bkClass("cl-svc", "Servicio", 1, WeightInferred, bkCat("k-s3", "Amazon S3", 0))},
		Questions:       []Question{bkQ("a", ref2("Servicio", "Amazon S3"))},
	}

	w, err := PlanDeleteQuestion(bkExam(), bank, "q-a", editNow)

	if err != nil || w.DeleteQuestionID != "q-a" || len(w.Weights) != 0 {
		t.Errorf("write = %+v, err %v", w, err)
	}
}

func TestPlanDeleteQuestionOfAnUnknownQuestionIsNotFound(t *testing.T) {
	_, err := PlanDeleteQuestion(bkExam(), bkBank(), "q-nope", editNow)

	if !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
