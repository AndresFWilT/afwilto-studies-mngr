package domain

import (
	"slices"
	"testing"
)

func TestResolvePrimaryFollowsTheFourStepsOfFR31(t *testing.T) {
	usages := func(names ...string) []ClassificationUsage {
		out := make([]ClassificationUsage, len(names))
		for i, n := range names {
			out[i] = ClassificationUsage{Name: n, Eligible: true}
		}
		return out
	}
	cases := []struct {
		name     string
		existing string
		param    string
		usages   []ClassificationUsage
		want     string
		wantOK   bool
	}{
		{"1. the exam's own primary wins over everything", "Servicio", "Dominio", usages("Dominio", "Servicio"), "Servicio", true},
		{"1. even when it is not in this file", "Tema", "", usages("Dominio"), "Tema", true},
		{"2. the parameter names the primary", "", "Servicio", usages("Dominio", "Servicio"), "Servicio", true},
		{"2. matched by Fold: case, accents, spaces", "", "  módulo ", usages("Dominio", "Módulo"), "Módulo", true},
		{"2. the parameter wins over the default rule", "", "Servicio", usages("Dominio", "Servicio"), "Servicio", true},
		{"2. even when that classification is not eligible", "", "Servicio", []ClassificationUsage{{"Dominio", true}, {"Servicio", false}}, "Servicio", true},
		{"2. an unknown parameter falls through to the default rule", "", "Nada", usages("Dominio", "Servicio"), "Dominio", true},
		{"3. the first eligible in order of first appearance", "", "", usages("Dominio", "Servicio"), "Dominio", true},
		{"3. skipping classifications some question lists with two values", "", "", []ClassificationUsage{{"Servicio", false}, {"Dominio", true}}, "Dominio", true},
		{"4. none eligible: no primary", "", "", []ClassificationUsage{{"Dominio", false}, {"Servicio", false}}, "", false},
		{"4. no classifications at all", "", "", nil, "", false},
		{"4. no classifications, a parameter does not invent one", "", "Dominio", nil, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := ResolvePrimary(tc.existing, tc.param, tc.usages)
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("ResolvePrimary(%q, %q, %+v) = (%q, %v), want (%q, %v)", tc.existing, tc.param, tc.usages, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestSimilarNamesFiresAtDistanceTwoOrAnEqualSlug(t *testing.T) { // FR-3.2, AC-3
	existing := []string{"Alto rendimiento", "Seguridad", "Costos"}
	cases := []struct {
		name string
		in   string
		from []string
		want []string
	}{
		{"AC-3: two letters swapped", "Alto rendimeinto", existing, []string{"Alto rendimiento"}},
		{"distance 1", "Alto rendimient", existing, []string{"Alto rendimiento"}},
		{"distance 3 is not similar", "Alto rendim", existing, nil},
		{"case and accents do not count", "SEGURIDÁD", existing, []string{"Seguridad"}},
		{"unrelated", "Migración", existing, nil},
		{"equal slug beyond distance 2", "Amazon S3 --- Glacier", []string{"Amazon S3 / Glacier"}, []string{"Amazon S3 / Glacier"}},
		{"empty slugs never match each other", "???", []string{"!!!!!!!!"}, nil},
		{"several matches keep the existing order", "Costas", []string{"Costos", "Seguridad", "Costes"}, []string{"Costos", "Costes"}},
		{"no existing names", "Alto", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SimilarNames(tc.in, tc.from)
			if !slices.Equal(got, tc.want) && !(len(got) == 0 && len(tc.want) == 0) {
				t.Errorf("SimilarNames(%q, %v) = %v, want %v", tc.in, tc.from, got, tc.want)
			}
		})
	}
}
