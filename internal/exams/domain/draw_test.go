package domain

import (
	"errors"
	"slices"
	"testing"
)

func TestSimulationDrawsQuotasByLargestRemainder(t *testing.T) { // AC-14
	draw, err := DrawSession(sessExam(), awsBank(), simulation(), 42)
	if err != nil {
		t.Fatal(err)
	}

	got := byDomain(draw)
	want := map[string]int{"Seguridad": 19, "Resiliente": 17, "Rendimiento": 16, "Costos": 13}
	if len(draw.Questions) != 65 || !mapsEqual(got, want) {
		t.Errorf("drew %d questions, per domain %v; want 65 as %v (floors 19/16/15/13 + the two largest fractions .9 and .6)", len(draw.Questions), got, want)
	}
}

func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestSimulationNeverDrawsUnclassifiedOrWeightZeroQuestions(t *testing.T) { // FR-9, S06a-1
	for seed := uint64(0); seed < 20; seed++ {
		draw, err := DrawSession(sessExam(), awsBank(), simulation(), seed)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range draw.Questions {
			if d.Question.Categories[0].Category == "Legacy" || d.Question.Categories[0].Classification != "Dominio" {
				t.Fatalf("seed %d drew %s (%v): weight-0 and unclassified questions stay out of a simulation", seed, d.Question.Key, d.Question.Categories)
			}
		}
	}
}

func TestSimulationDrawsEachQuestionAtMostOnce(t *testing.T) { // FR-9 "without replacement"
	draw, _ := DrawSession(sessExam(), awsBank(), simulation(), 7)
	if len(draw.Questions) != 65 {
		t.Fatalf("drew %d, want 65", len(draw.Questions))
	}
	seen := map[string]bool{}
	for _, d := range draw.Questions {
		if seen[d.Question.ID] {
			t.Fatalf("%s drawn twice", d.Question.Key)
		}
		seen[d.Question.ID] = true
	}
}

func TestSimulationMixesTheCategoriesInsteadOfGroupingThem(t *testing.T) { // FR-9 "questions are shuffled"
	draw, _ := DrawSession(sessExam(), awsBank(), simulation(), 11)
	changes := 0
	for i := 1; i < len(draw.Questions); i++ {
		if draw.Questions[i].Question.Categories[0] != draw.Questions[i-1].Question.Categories[0] {
			changes++
		}
	}
	if changes < 20 {
		t.Errorf("only %d category changes in 65 questions: the draw is still grouped by category", changes)
	}
}

func TestSameSeedOnAnUnchangedBankRepeatsQuestionsAndOptionOrder(t *testing.T) { // AC-17
	a, _ := DrawSession(sessExam(), awsBank(), simulation(), 12345)
	b, _ := DrawSession(sessExam(), awsBank(), simulation(), 12345)

	if drawnKeys(a) == "" || drawnKeys(a) != drawnKeys(b) {
		t.Errorf("same seed, different questions:\n%s\n%s", drawnKeys(a), drawnKeys(b))
	}
	if optionTexts(a) != optionTexts(b) {
		t.Error("same seed, different option order")
	}
}

func TestDifferentSeedsGiveDifferentDraws(t *testing.T) { // AC-17 counter-check: the seed is what drives the draw
	base, _ := DrawSession(sessExam(), awsBank(), simulation(), 1)
	for seed := uint64(2); seed < 12; seed++ {
		other, _ := DrawSession(sessExam(), awsBank(), simulation(), seed)
		if drawnKeys(other) != drawnKeys(base) {
			return
		}
	}
	t.Error("ten different seeds all drew the same questions in the same order")
}

func TestTheDrawDependsOnTheSeedOnlyNotOnTheBankOrder(t *testing.T) { // FR-9: shuffles start from the id-sorted pool
	bank := awsBank()
	reversed := bank
	reversed.Questions = slices.Clone(bank.Questions)
	slices.Reverse(reversed.Questions)

	a, _ := DrawSession(sessExam(), bank, simulation(), 99)
	b, _ := DrawSession(sessExam(), reversed, simulation(), 99)

	if len(a.Questions) != 65 || drawnKeys(a) != drawnKeys(b) {
		t.Error("the same seed drew different questions when the bank was read in another order")
	}
}

func TestOptionsAreRelabelledInShuffledOrderAndKeepTheirCorrectFlag(t *testing.T) { // FR-9 order
	reordered := false
	for seed := uint64(0); seed < 10; seed++ {
		draw, _ := DrawSession(sessExam(), awsBank(), practice(Selection{Kind: SelectRandom, Count: 40}), seed)
		for _, d := range draw.Questions {
			texts := make([]string, len(d.Options))
			for i, o := range d.Options {
				texts[i] = o.Text
				if o.Correct != (o.Text == d.Question.Key+"/b") {
					t.Fatalf("%s: option %q has correct=%v after the shuffle", d.Question.Key, o.Text, o.Correct)
				}
			}
			if len(d.Options) != 4 {
				t.Fatalf("%s: %d options", d.Question.Key, len(d.Options))
			}
			if !slices.Equal(texts, []string{d.Question.Key + "/a", d.Question.Key + "/b", d.Question.Key + "/c", d.Question.Key + "/d"}) {
				reordered = true
			}
		}
	}
	if !reordered {
		t.Error("options are never shuffled")
	}
}

func TestInsufficientQuestionsNamesTheShortCategory(t *testing.T) { // AC-15
	exam := sessExam()
	exam.QuestionCount = 100
	bank := Bank{Classifications: []Classification{
		bkClass("cl", "Dominio", PositionPrimary, WeightManual, bkCat("a", "Seguridad", 20), bkCat("b", "Costos", 80)),
	}}
	bank.Questions = append(manyQ("seg", 12, dom("Seguridad")), manyQ("cos", 90, dom("Costos"))...)

	_, err := DrawSession(exam, bank, simulation(), 1)

	want := []Shortage{{Category: "Seguridad", Needed: 20, Available: 12}}
	if got := shortagesOf(err); !slices.Equal(got, want) {
		t.Errorf("shortages = %v (err %v), want %v", got, err, want)
	}
}

func TestInsufficientQuestionsListsEveryShortCategoryInCategoryOrder(t *testing.T) { // FR-9 "for each one"
	exam := sessExam()
	exam.QuestionCount = 100
	bank := Bank{Classifications: []Classification{
		bkClass("cl", "Dominio", PositionPrimary, WeightManual, bkCat("a", "Seguridad", 50), bkCat("b", "Costos", 50)),
	}}
	bank.Questions = append(manyQ("seg", 0, dom("Seguridad")), manyQ("cos", 49, dom("Costos"))...)

	_, err := DrawSession(exam, bank, simulation(), 1)

	want := []Shortage{{"Seguridad", 50, 0}, {"Costos", 50, 49}}
	if got := shortagesOf(err); !slices.Equal(got, want) {
		t.Errorf("shortages = %v, want %v", got, want)
	}
}

func TestSimulationWithoutAnyWeightedCategoryIsInsufficient(t *testing.T) { // slice 06a §5
	bank := Bank{Classifications: []Classification{
		bkClass("cl", "Dominio", PositionPrimary, WeightInferred, bkCat("a", "Seguridad", 0)),
	}, Questions: manyQ("seg", 5, dom("Seguridad"))}

	_, err := DrawSession(sessExam(), bank, simulation(), 1)

	want := []Shortage{{Category: "weighted categories", Needed: 65, Available: 0}}
	if got := shortagesOf(err); !slices.Equal(got, want) {
		t.Errorf("shortages = %v, want %v", got, want)
	}
}

func TestSimulationWithoutAPrimaryClassificationIsInsufficient(t *testing.T) {
	bank := Bank{Classifications: []Classification{bkClass("cl", "Servicio", PositionSecondary, WeightInferred, bkCat("a", "VPC", 0))},
		Questions: manyQ("v", 5, svc("VPC"))}

	_, err := DrawSession(sessExam(), bank, simulation(), 1)

	if len(shortagesOf(err)) != 1 {
		t.Errorf("err = %v, want one insufficient_questions detail", err)
	}
}

func TestSimulationSkipsCategoriesWhoseQuotaRoundsToZero(t *testing.T) {
	exam := sessExam()
	exam.QuestionCount = 1
	bank := Bank{Classifications: []Classification{
		bkClass("cl", "Dominio", PositionPrimary, WeightManual, bkCat("a", "Seguridad", 50), bkCat("b", "Costos", 50)),
	}, Questions: manyQ("seg", 3, dom("Seguridad"))}

	draw, err := DrawSession(exam, bank, simulation(), 1)

	if err != nil || len(draw.Questions) != 1 || draw.Questions[0].Question.Categories[0].Category != "Seguridad" {
		t.Errorf("draw = %v, err %v; want the single question from the first tied category and no shortage for the empty one", drawnKeys(draw), err)
	}
}

func TestRandomTakesCountFromTheWholeBankIncludingUnclassified(t *testing.T) { // FR-7 random
	draw, err := DrawSession(sessExam(), awsBank(), practice(Selection{Kind: SelectRandom, Count: 40}), 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(draw.Questions) != 40 {
		t.Errorf("drew %d, want 40", len(draw.Questions))
	}
	seen := map[string]bool{}
	for seed := uint64(0); seed < 30; seed++ {
		d, _ := DrawSession(sessExam(), awsBank(), practice(Selection{Kind: SelectRandom, Count: 40}), seed)
		for _, q := range d.Questions {
			seen[q.Question.Categories[0].Category] = true
		}
	}
	for _, name := range []string{"Legacy", "VPC"} {
		if !seen[name] {
			t.Errorf("random never drew a %s question in 30 seeds: the pool is the whole bank", name)
		}
	}
}

func TestACountAboveThePoolDrawsTheWholePool(t *testing.T) { // slice 06a §5
	bank := Bank{Questions: manyQ("x", 6)}
	draw, err := DrawSession(sessExam(), bank, practice(Selection{Kind: SelectRandom, Count: 40}), 1)
	if err != nil || len(draw.Questions) != 6 {
		t.Errorf("drew %d, err %v; want all 6", len(draw.Questions), err)
	}
}

func TestRandomFromAnEmptyBankIsInsufficient(t *testing.T) {
	_, err := DrawSession(sessExam(), Bank{}, practice(Selection{Kind: SelectRandom, Count: 5}), 1)
	want := []Shortage{{Category: "bank", Needed: 1, Available: 0}}
	if got := shortagesOf(err); !slices.Equal(got, want) {
		t.Errorf("shortages = %v, want %v", got, want)
	}
}

func categoriesSelection(bank Bank, class string, names ...string) Selection {
	sel := Selection{Kind: SelectCategories, All: true}
	for _, c := range bank.Classifications {
		if c.Name == class {
			sel.ClassificationID = c.ID
			for _, k := range c.Categories {
				if slices.Contains(names, k.Name) {
					sel.CategoryIDs = append(sel.CategoryIDs, k.ID)
				}
			}
		}
	}
	return sel
}

func TestCategoriesDrawsOnlyFromTheChosenCategories(t *testing.T) { // FR-7
	bank := awsBank()
	sel := categoriesSelection(bank, "Dominio", "Seguridad", "Costos")
	sel.All, sel.Count = false, 20

	draw, err := DrawSession(sessExam(), bank, practice(sel), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(draw.Questions) != 20 {
		t.Fatalf("drew %d, want 20", len(draw.Questions))
	}
	for _, d := range draw.Questions {
		if c := d.Question.Categories[0].Category; c != "Seguridad" && c != "Costos" {
			t.Errorf("drew a %s question", c)
		}
	}
}

func TestCategoriesAllTakesEveryQuestionOfTheChosenCategoriesOnce(t *testing.T) { // FR-7 "all"
	bank := awsBank()
	multi := sessQ("both", dom("Seguridad"), dom("Costos"))
	bank.Questions = append(bank.Questions, multi)
	draw, err := DrawSession(sessExam(), bank, practice(categoriesSelection(bank, "Dominio", "Seguridad", "Costos")), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(draw.Questions) != 61 {
		t.Errorf("drew %d, want 30+30+1 (a question in both chosen categories counts once)", len(draw.Questions))
	}
}

func TestCategoriesWithAnUnknownClassificationOrCategoryIsNotFound(t *testing.T) {
	bank := awsBank()
	cases := map[string]Selection{
		"unknown classification":             {Kind: SelectCategories, ClassificationID: "nope", CategoryIDs: []string{"c-seg"}, All: true},
		"unknown category":                   {Kind: SelectCategories, ClassificationID: "cl-dom", CategoryIDs: []string{"c-seg", "ghost"}, All: true},
		"category of another classification": {Kind: SelectCategories, ClassificationID: "cl-dom", CategoryIDs: []string{"c-vpc"}, All: true},
	}
	for name, sel := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := DrawSession(sessExam(), bank, practice(sel), 1)
			var v *ValidationError
			if !errors.As(err, &v) || v.Violations[0].Rule != RuleNotFound {
				t.Errorf("err = %v, want a not_found validation error", err)
			}
		})
	}
}

func TestOriginSetKeepsOriginNumberThenKeyOrder(t *testing.T) { // AC-21
	bank := Bank{Questions: []Question{
		withOrigin(sessQ("c"), "Timed Set 3", 3),
		withOrigin(sessQ("a"), "Timed Set 3", 1),
		withOrigin(sessQ("z2"), "Timed Set 3", 2),
		withOrigin(sessQ("z1"), "Timed Set 3", 2),
		withOrigin(sessQ("other"), "Timed Set 4", 1),
		sessQ("no-origin"),
	}}
	for _, seed := range []uint64{1, 2, 3} {
		draw, err := DrawSession(sessExam(), bank, practice(Selection{Kind: SelectOriginSet, Set: "Timed Set 3"}), seed)
		if err != nil {
			t.Fatal(err)
		}
		if got := drawnKeys(draw); got != "a,z1,z2,c" {
			t.Errorf("seed %d: order %s, want a,z1,z2,c (origin.number, then key)", seed, got)
		}
	}
}

func TestOriginSetPutsQuestionsWithoutANumberLastThenByKey(t *testing.T) { // phase-3 fix 6, as in the bank browser
	bank := Bank{Questions: []Question{
		withOrigin(sessQ("b-unnumbered"), "Timed Set 3", 0),
		withOrigin(sessQ("z-numbered-2"), "Timed Set 3", 2),
		withOrigin(sessQ("a-unnumbered"), "Timed Set 3", 0),
		withOrigin(sessQ("y-numbered-1"), "Timed Set 3", 1),
		withOrigin(sessQ("x-numbered-1"), "Timed Set 3", 1),
	}}
	for _, seed := range []uint64{1, 2, 3} {
		draw, err := DrawSession(sessExam(), bank, practice(Selection{Kind: SelectOriginSet, Set: "Timed Set 3"}), seed)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := drawnKeys(draw), "x-numbered-1,y-numbered-1,z-numbered-2,a-unnumbered,b-unnumbered"; got != want {
			t.Errorf("seed %d: order %s, want %s", seed, got, want)
		}
	}
}

func TestOriginSetStillShufflesItsOptions(t *testing.T) { // FR-9 "Options are always shuffled"
	bank := Bank{Questions: []Question{}}
	for i := 1; i <= 20; i++ {
		bank.Questions = append(bank.Questions, withOrigin(sessQ("q"+string(rune('a'+i))), "Set", i))
	}
	draw, _ := DrawSession(sessExam(), bank, practice(Selection{Kind: SelectOriginSet, Set: "Set"}), 8)
	for _, d := range draw.Questions {
		if d.Options[0].Text != d.Question.Key+"/a" {
			return
		}
	}
	t.Error("no question in the set had its options moved")
}

func TestAnEmptyOriginSetIsInsufficient(t *testing.T) { // slice 06a §1.3
	_, err := DrawSession(sessExam(), Bank{Questions: manyQ("x", 3)}, practice(Selection{Kind: SelectOriginSet, Set: "Timed Set 9"}), 1)
	want := []Shortage{{Category: "Timed Set 9", Needed: 1, Available: 0}}
	if got := shortagesOf(err); !slices.Equal(got, want) {
		t.Errorf("shortages = %v, want %v", got, want)
	}
}

func TestOriginSetMatchesTheSetNameExactly(t *testing.T) {
	bank := Bank{Questions: []Question{withOrigin(sessQ("a"), "Timed Set 3", 1)}}
	if _, err := DrawSession(sessExam(), bank, practice(Selection{Kind: SelectOriginSet, Set: "timed set 3"}), 1); err == nil {
		t.Error("a set name that differs in case matched")
	}
}
