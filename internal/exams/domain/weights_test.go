package domain

import (
	"math"
	"slices"
	"strings"
	"testing"
)

// bkCat and the helpers below build the fixtures of the slice 04 tests.
func bkCat(id, name string, weight int) Category {
	return Category{ID: id, Key: Slug(name), Name: name, Fold: Fold(name), WeightPercent: weight}
}

func bkClass(id, name string, position int, mode WeightMode, cats ...Category) Classification {
	return Classification{ID: id, ExamID: "exam-1", Name: name, Fold: Fold(name), Position: position, WeightMode: mode, Categories: cats}
}

func bkQ(key string, refs ...CategoryRef) Question {
	return Question{ID: "q-" + key, ExamID: "exam-1", Key: key, Type: QuestionSingle, Stem: "stem " + key, Version: 1, Categories: refs}
}

func ref2(classification, category string) CategoryRef {
	return CategoryRef{Classification: classification, Category: category}
}

func weightsOf(ws []CategoryWeight) map[string]int {
	out := map[string]int{}
	for _, w := range ws {
		out[w.CategoryID] = w.Percent
	}
	return out
}

func TestInferWeightsGivesEachCategoryItsShareWithLargestRemainder(t *testing.T) { // AC-6
	cats := []Category{bkCat("a", "Alto rendimiento", 0), bkCat("b", "Seguridad", 0), bkCat("c", "Costos", 0)}

	got := InferWeights(cats, map[string]int{"a": 5, "b": 3, "c": 3})

	want := []CategoryWeight{{"a", 46}, {"b", 27}, {"c", 27}}
	if !slices.Equal(got, want) {
		t.Errorf("InferWeights = %v, want %v (raw 45.45 / 27.27 / 27.27)", got, want)
	}
}

func TestInferWeightsKeepsCategoryOrderAndGivesZeroToEmptyCategories(t *testing.T) {
	cats := []Category{bkCat("a", "A", 0), bkCat("b", "B", 0), bkCat("c", "C", 0)}

	got := InferWeights(cats, map[string]int{"a": 2, "c": 2})

	want := []CategoryWeight{{"a", 50}, {"b", 0}, {"c", 50}}
	if !slices.Equal(got, want) {
		t.Errorf("InferWeights = %v, want %v", got, want)
	}
}

func TestInferWeightsWithNoClassifiedQuestionsIsAllZero(t *testing.T) {
	cats := []Category{bkCat("a", "A", 40), bkCat("b", "B", 60)}

	got := InferWeights(cats, nil)

	want := []CategoryWeight{{"a", 0}, {"b", 0}}
	if !slices.Equal(got, want) {
		t.Errorf("InferWeights = %v, want %v", got, want)
	}
}

func TestInferWeightsOfNoCategoriesIsEmpty(t *testing.T) {
	if got := InferWeights(nil, map[string]int{"a": 3}); len(got) != 0 {
		t.Errorf("InferWeights = %v, want none", got)
	}
}

func TestLinkCountsMatchesReferencesByFoldedNames(t *testing.T) {
	bank := Bank{
		Classifications: []Classification{
			bkClass("cl-1", "Dominio", 0, WeightInferred, bkCat("k-1", "Señales", 0), bkCat("k-2", "Costos", 0)),
			bkClass("cl-2", "Servicio", 1, WeightInferred, bkCat("k-3", "Costos", 0)),
		},
		Questions: []Question{
			bkQ("a", ref2("Dominio", "Señales"), ref2("Servicio", "Costos")),
			bkQ("b", ref2("dominio", "SEÑALES")),
			bkQ("c", ref2("Dominio", "Costos")),
			bkQ("d", ref2("Dominio", "Desconocida")),
		},
	}

	got := bank.LinkCounts()

	want := map[string]int{"k-1": 2, "k-2": 1, "k-3": 1}
	for id, n := range want {
		if got[id] != n {
			t.Errorf("LinkCounts[%s] = %d, want %d (all: %v)", id, got[id], n, got)
		}
	}
}

func TestValidateManualWeights(t *testing.T) { // S04-3, BR-C3
	primary := []Category{bkCat("k-seg", "Seguridad", 0), bkCat("k-cos", "Costos", 0), bkCat("k-ren", "Rendimiento", 0), bkCat("k-res", "Resiliencia", 0)}
	w := func(pairs ...any) []WeightInput {
		var out []WeightInput
		for i := 0; i < len(pairs); i += 2 {
			out = append(out, WeightInput{CategoryID: pairs[i].(string), Percent: pairs[i+1].(int)})
		}
		return out
	}
	cases := []struct {
		name        string
		weights     []WeightInput
		wantField   string // "" means valid
		wantRule    string
		wantMessage string
	}{
		{"30/26/24/20 sums to 100", w("k-seg", 30, "k-cos", 26, "k-ren", 24, "k-res", 20), "", "", ""},
		{"a zero weight is allowed", w("k-seg", 50, "k-cos", 50, "k-ren", 0, "k-res", 0), "", "", ""},
		{"S04-3: 99 is not 100", w("k-seg", 30, "k-cos", 26, "k-ren", 24, "k-res", 19), "weights", RuleWeightsSum, "99"},
		{"101 is not 100", w("k-seg", 30, "k-cos", 26, "k-ren", 24, "k-res", 21), "weights", RuleWeightsSum, "101"},
		{"S04-3: a skipped category is named", w("k-seg", 40, "k-cos", 30, "k-ren", 30), "weights", RuleWeightsSum, "k-res"},
		{"a negative weight", w("k-seg", 110, "k-cos", -10, "k-ren", 0, "k-res", 0), "weights[1].percent", RuleRange, "-10"},
		{"a weight above 100", w("k-seg", 101, "k-cos", 0, "k-ren", 0, "k-res", 0), "weights[0].percent", RuleRange, "101"},
		{"a weight of 3,000,000,000", w("k-seg", 3_000_000_000, "k-cos", 0, "k-ren", 0, "k-res", 0), "weights[0].percent", RuleRange, "3000000000"},
		{"an unknown category", w("k-seg", 50, "k-cos", 50, "k-ren", 0, "k-res", 0, "k-zzz", 0), "weights[4].categoryId", RuleNotFound, "k-zzz"},
		{"a category listed twice", w("k-seg", 50, "k-seg", 50, "k-cos", 0, "k-ren", 0, "k-res", 0), "weights[1].categoryId", "format", "k-seg"},
		{"nothing at all", nil, "weights", RuleWeightsSum, "k-seg"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ValidateManualWeights(primary, tc.weights)

			if tc.wantField == "" {
				if len(got) != 0 {
					t.Fatalf("violations = %+v, want none", got)
				}
				return
			}
			for _, v := range got {
				if v.Field == tc.wantField && v.Rule == tc.wantRule && strings.Contains(v.Message, tc.wantMessage) {
					return
				}
			}
			t.Errorf("violations = %+v, want %s/%s mentioning %q", got, tc.wantField, tc.wantRule, tc.wantMessage)
		})
	}
}

func TestManualWeightsThatWrapAroundTo100AreRangeErrorsNotAccepted(t *testing.T) { // phase-2 fix 3
	primary := []Category{{ID: "k-seg"}, {ID: "k-cos"}, {ID: "k-ren"}, {ID: "k-res"}}
	// the old code added the percents as they came: MaxInt64 + MaxInt64 + 102 wraps to 100 (mod 2^64)
	wrapped := []WeightInput{
		{CategoryID: "k-seg", Percent: math.MaxInt64}, {CategoryID: "k-cos", Percent: math.MaxInt64},
		{CategoryID: "k-ren", Percent: 102}, {CategoryID: "k-res", Percent: 0},
	}

	got := ValidateManualWeights(primary, wrapped)

	fields := map[string]bool{}
	for _, v := range got {
		if v.Rule != RuleRange {
			t.Errorf("violation %+v, want only range errors", v)
		}
		fields[v.Field] = true
	}
	if !fields["weights[0].percent"] || !fields["weights[1].percent"] || !fields["weights[2].percent"] || len(got) != 3 {
		t.Errorf("violations = %+v, want range at weights[0], weights[1] and weights[2] (102 is above 100 too)", got)
	}
}

func TestManualWeightsAtTheEdgesOfTheRangeAreAccepted(t *testing.T) { // phase-2 fix 3 boundary
	primary := []Category{{ID: "k-seg"}, {ID: "k-cos"}}

	got := ValidateManualWeights(primary, []WeightInput{{CategoryID: "k-seg", Percent: 100}, {CategoryID: "k-cos", Percent: 0}})

	if len(got) != 0 {
		t.Errorf("violations = %+v, want none", got)
	}
}
