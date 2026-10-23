package domain

import (
	"math"
	"testing"
)

// paceBank is the AC-16 fixture: VPC paces 10 questions in 20 min (2 min each),
// Lambda 4 in 10 (2.5 each); 10 VPC and 4 Lambda questions.
func paceBank() Bank {
	vpc, lambda := bkCat("c-vpc", "VPC", 0), bkCat("c-lam", "Lambda", 0)
	ten, twenty, four := 10, 20, 4
	vpc.PaceQuestions, vpc.PaceMinutes = &ten, &twenty
	lambda.PaceQuestions, lambda.PaceMinutes = &four, &ten
	servicio := bkClass("cl-svc", "Servicio", PositionSecondary, WeightInferred, vpc, lambda)
	var qs []Question
	qs = append(qs, manyQ("vpc", 10, svc("VPC"))...)
	qs = append(qs, manyQ("lam", 4, svc("Lambda"))...)
	return Bank{Classifications: []Classification{servicio}, Questions: qs}
}

func serviceSelection(bank Bank, count int) Selection {
	sel := categoriesSelection(bank, "Servicio", "VPC", "Lambda")
	if count > 0 {
		sel.All, sel.Count = false, count
	}
	return sel
}

func TestQuestionPaceUsesTheCategoryPaceOrTheExamPace(t *testing.T) { // FR-8
	bank := paceBank()
	svcClass := &bank.Classifications[0]
	exam := sessExam() // 130 min / 65 questions = 2 min

	for name, tc := range map[string]struct {
		q    Question
		want float64
	}{
		"VPC: 20 min / 10 questions":              {sessQ("a", svc("VPC")), 2},
		"Lambda: 10 min / 4 questions":            {sessQ("b", svc("Lambda")), 2.5},
		"largest pace among its categories":       {sessQ("c", svc("VPC"), svc("Lambda")), 2.5},
		"no category here: the exam pace":         {sessQ("d", dom("Seguridad")), 2},
		"no category at all: the exam pace":       {sessQ("e"), 2},
		"a category without its own pace: exam's": {sessQ("f", svc("Nueva")), 2},
	} {
		if got := QuestionPace(exam, svcClass, tc.q); got != tc.want {
			t.Errorf("%s: pace = %v, want %v", name, got, tc.want)
		}
	}
}

func TestQuestionPaceWithoutAClassificationIsTheExamPace(t *testing.T) { // FR-8, exam without a primary
	exam := sessExam()
	exam.TimeLimitMin, exam.QuestionCount = 45, 50
	if got := QuestionPace(exam, nil, sessQ("a", dom("Seguridad"))); got != 0.9 {
		t.Errorf("pace = %v, want 45/50 = 0.9", got)
	}
}

func TestCeilMinutesAbsorbsFloatNoise(t *testing.T) { // slice 06a §5
	sum := 0.0
	for range 6 {
		sum += 7.0 / 3 // 14.000000000000002 in float64
	}
	if sum == 14 {
		t.Fatal("the fixture no longer produces float noise")
	}
	for in, want := range map[float64]int{10 + 1e-12: 10, 10: 10, 10.01: 11, 0: 0, 14.5: 15, 29.999999999999996: 30} {
		if got := CeilMinutes(in); got != want {
			t.Errorf("CeilMinutes(%v) = %d, want %d", in, got, want)
		}
	}
	if got := CeilMinutes(sum); got != 14 {
		t.Errorf("CeilMinutes(6 × 7/3) = %d, want 14", got)
	}
}

func TestPreviewOfAllQuestionsOfTwoCategoriesIsTheExactSum(t *testing.T) { // S06a-2, AC-16 first case
	bank := paceBank()

	p, err := PreviewSession(sessExam(), bank, practice(serviceSelection(bank, 0)))
	if err != nil {
		t.Fatal(err)
	}

	if p.PoolSize != 14 || p.DrawCount != 14 || p.SuggestedMinutes != 30 || len(p.Shortages) != 0 {
		t.Errorf("preview = %+v, want pool 14, draw 14, 30 min (10×2 + 4×2.5), no shortages", p)
	}
}

func TestPreviewOfFewerThanThePoolIsCeilOfNTimesTheMeanPace(t *testing.T) { // slice 06a §1.2
	bank := paceBank() // mean pace 30/14

	p, _ := PreviewSession(sessExam(), bank, practice(serviceSelection(bank, 5)))

	if p.PoolSize != 14 || p.DrawCount != 5 || p.SuggestedMinutes != 11 { // 5 × 30/14 = 10.71
		t.Errorf("preview = %+v, want pool 14, draw 5, ceil(10.71) = 11", p)
	}
}

func TestPreviewOfACountAboveThePoolIsTheWholePoolSum(t *testing.T) {
	bank := paceBank()
	p, _ := PreviewSession(sessExam(), bank, practice(serviceSelection(bank, 50)))
	if p.DrawCount != 14 || p.SuggestedMinutes != 30 {
		t.Errorf("preview = %+v, want the whole pool: draw 14, 30 min", p)
	}
}

func TestPreviewOfARandomSessionPricesWithThePrimaryClassification(t *testing.T) { // FR-8
	bank := awsBank() // no paces: the exam pace of 2 min
	p, _ := PreviewSession(sessExam(), bank, practice(Selection{Kind: SelectRandom, Count: 40}))
	if p.PoolSize != 140 || p.DrawCount != 40 || p.SuggestedMinutes != 80 {
		t.Errorf("preview = %+v, want pool 140, draw 40, 40 × 2 = 80", p)
	}
}

func TestPreviewOfAnOriginSetIsTheWholeSet(t *testing.T) {
	bank := Bank{Questions: []Question{withOrigin(sessQ("a"), "S", 1), withOrigin(sessQ("b"), "S", 2), withOrigin(sessQ("c"), "T", 1)}}
	p, _ := PreviewSession(sessExam(), bank, practice(Selection{Kind: SelectOriginSet, Set: "S"}))
	if p.PoolSize != 2 || p.DrawCount != 2 || p.SuggestedMinutes != 4 {
		t.Errorf("preview = %+v, want 2 questions × 2 min", p)
	}
}

func TestPreviewOfASimulationOnTheAC14Fixture(t *testing.T) { // S06a-1
	p, err := PreviewSession(sessExam(), awsBank(), simulation())
	if err != nil {
		t.Fatal(err)
	}
	if p.PoolSize != 120 || p.DrawCount != 65 || p.SuggestedMinutes != 130 || len(p.Shortages) != 0 {
		t.Errorf("preview = %+v, want pool 120 (weighted categories only), draw 65, 130 min, no shortages", p)
	}
}

func TestPreviewListsTheSameShortagesTheStartFailsOn(t *testing.T) { // slice 06a §1.2
	exam := sessExam()
	exam.QuestionCount = 100
	bank := Bank{Classifications: []Classification{
		bkClass("cl", "Dominio", PositionPrimary, WeightManual, bkCat("a", "Seguridad", 20), bkCat("b", "Costos", 80)),
	}, Questions: append(manyQ("seg", 12, dom("Seguridad")), manyQ("cos", 90, dom("Costos"))...)}

	p, err := PreviewSession(exam, bank, simulation())
	if err != nil {
		t.Fatalf("a preview with shortages is a successful read, got %v", err)
	}
	_, startErr := DrawSession(exam, bank, simulation(), 1)

	want := []Shortage{{"Seguridad", 20, 12}}
	if len(p.Shortages) != 1 || p.Shortages[0] != want[0] || shortagesOf(startErr)[0] != want[0] {
		t.Errorf("preview shortages %v, start shortages %v; want both %v", p.Shortages, shortagesOf(startErr), want)
	}
	if p.DrawCount != 100 || p.SuggestedMinutes != 130 {
		t.Errorf("preview = %+v, want the intended draw of 100 questions and the exam time", p)
	}
}

func TestPreviewOfAnEmptyPoolReportsTheShortageAndZeroMinutes(t *testing.T) {
	p, err := PreviewSession(sessExam(), Bank{}, practice(Selection{Kind: SelectRandom, Count: 5}))
	if err != nil || p.PoolSize != 0 || p.DrawCount != 0 || p.SuggestedMinutes != 0 || len(p.Shortages) != 1 {
		t.Errorf("preview = %+v, err %v; want an empty pool, 0 min and the 'banco' shortage", p, err)
	}
}

// findFiveAndTwo looks for a seed whose 7-question draw is 5 VPC + 2 Lambda.
func findFiveAndTwo(t *testing.T, bank Bank) uint64 {
	t.Helper()
	for seed := uint64(0); seed < 5000; seed++ {
		draw, err := DrawSession(sessExam(), bank, practice(serviceSelection(bank, 7)), seed)
		if err != nil {
			t.Fatal(err)
		}
		lam := 0
		for _, d := range draw.Questions {
			if d.Question.Categories[0].Category == "Lambda" {
				lam++
			}
		}
		if lam == 2 {
			return seed
		}
	}
	t.Fatal("no seed in 5000 draws 5 VPC + 2 Lambda")
	return 0
}

func TestStartSuggestsTheCeilOfTheDrawnPaces(t *testing.T) { // S06a-3, AC-16 second case
	bank := paceBank()
	seed := findFiveAndTwo(t, bank)

	draw, err := DrawSession(sessExam(), bank, practice(serviceSelection(bank, 7)), seed)
	if err != nil {
		t.Fatal(err)
	}

	if draw.SuggestedMinutes != 15 { // ⌈5×2 + 2×2.5⌉
		t.Errorf("seed %d: suggested = %d, want 15", seed, draw.SuggestedMinutes)
	}
}

func TestStartSuggestsWhatTheDrawnCompositionCosts(t *testing.T) { // FR-8 over many seeds
	bank := paceBank()
	for seed := uint64(0); seed < 60; seed++ {
		draw, _ := DrawSession(sessExam(), bank, practice(serviceSelection(bank, 7)), seed)
		lam := 0
		for _, d := range draw.Questions {
			if d.Question.Categories[0].Category == "Lambda" {
				lam++
			}
		}
		want := int(math.Ceil(float64(7-lam)*2 + float64(lam)*2.5))
		if draw.SuggestedMinutes != want {
			t.Fatalf("seed %d with %d Lambda: suggested %d, want %d", seed, lam, draw.SuggestedMinutes, want)
		}
	}
}

func TestStartOfASimulationSuggestsTheExamTime(t *testing.T) { // FR-8
	draw, _ := DrawSession(sessExam(), awsBank(), simulation(), 1)
	if draw.SuggestedMinutes != 130 {
		t.Errorf("suggested = %d, want the exam's 130", draw.SuggestedMinutes)
	}
}
