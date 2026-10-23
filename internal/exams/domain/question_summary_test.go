package domain

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

func TestStemExcerptStripsMarkdownSyntax(t *testing.T) { // slice 04 §1.1
	cases := []struct{ name, stem, want string }{
		{"plain text is kept", "¿Qué servicio almacena objetos?", "¿Qué servicio almacena objetos?"},
		{"bold and italic markers go", "Un **bucket** es *regional* y __único__ y _global_", "Un bucket es regional y único y global"},
		{"strikethrough and inline code", "Usa ~~IAM~~ `aws s3 ls` ya", "Usa IAM aws s3 ls ya"},
		{"a link keeps its text", "Lee [la guía](https://example.com/x) hoy", "Lee la guía hoy"},
		{"an image keeps its alt text", "¿Qué señal es?\n\n![Señal de alto](media:img/r101.png)", "¿Qué señal es? Señal de alto"},
		{"headings, quotes and list markers go", "# Título\n> cita\n- uno\n* dos\n1. tres", "Título cita uno dos tres"},
		{"code fence lines go, their content stays", "Mira:\n```bash\naws s3 ls\n```\nfin", "Mira: aws s3 ls fin"},
		{"newlines and runs of spaces collapse", "uno\n\n\ndos   tres\t\tcuatro", "uno dos tres cuatro"},
		{"underscores inside identifiers stay", "El bucket my_bucket_name es privado", "El bucket my_bucket_name es privado"},
		{"a lone asterisk stays", "2 * 3 es 6", "2 * 3 es 6"},
		{"leading and trailing space goes", "  \n hola \n ", "hola"},
		{"an empty stem", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := StemExcerpt(tc.stem); got != tc.want {
				t.Errorf("StemExcerpt(%q) = %q, want %q", tc.stem, got, tc.want)
			}
		})
	}
}

func TestStemExcerptTakesTheFirst200CharactersOfTheStrippedText(t *testing.T) {
	long := "**" + strings.Repeat("ñ", 250) + "**"

	got := StemExcerpt(long)

	if utf8.RuneCountInString(got) != 200 || strings.Trim(got, "ñ") != "" {
		t.Errorf("excerpt has %d characters (%.20q...), want 200 characters of the text without its markers", utf8.RuneCountInString(got), got)
	}
}

func TestStemExcerptOf200CharactersIsNotShortened(t *testing.T) {
	stem := strings.Repeat("a", 200)
	if got := StemExcerpt(stem); got != stem {
		t.Errorf("excerpt has %d characters, want 200", utf8.RuneCountInString(got))
	}
}

func TestSummarizeQuestionCarriesTheBrowserFields(t *testing.T) {
	updated := time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)
	q := Question{
		ID: "q-1", Key: "td-3-001", Type: QuestionMulti, Stem: "**Elige** dos", Difficulty: "hard",
		Origin:     &Origin{Set: "Timed Set 3", Number: 7},
		Categories: []CategoryRef{ref2("Dominio", "Seguridad"), ref2("Servicio", "IAM")},
		Options:    []Option{{Text: "no se copia"}}, Version: 3, UpdatedAt: updated,
	}

	got := SummarizeQuestion(q)

	if got.ID != "q-1" || got.Key != "td-3-001" || got.Type != QuestionMulti || got.StemExcerpt != "Elige dos" ||
		got.Difficulty != "hard" || got.Origin == nil || got.Origin.Set != "Timed Set 3" || got.Origin.Number != 7 ||
		len(got.Categories) != 2 || got.Categories[1] != ref2("Servicio", "IAM") || got.Version != 3 || !got.UpdatedAt.Equal(updated) {
		t.Errorf("SummarizeQuestion = %+v", got)
	}
}

func TestQuestionFilterViolations(t *testing.T) {
	cases := []struct {
		name   string
		filter QuestionFilter
		want   string // "" means valid
	}{
		{"no filter", QuestionFilter{}, ""},
		{"easy, medium and hard are allowed", QuestionFilter{Difficulty: "medium"}, ""},
		{"every filter at once", QuestionFilter{CategoryID: "k-1", Origin: "Timed Set 3", Difficulty: "easy", Text: "vpc", Unclassified: true}, ""},
		{"an unknown difficulty", QuestionFilter{Difficulty: "impossible"}, "difficulty"},
		{"difficulty is case sensitive like the contract enum", QuestionFilter{Difficulty: "Easy"}, "difficulty"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.filter.Violations()
			if tc.want == "" {
				if len(got) != 0 {
					t.Fatalf("violations = %+v, want none", got)
				}
				return
			}
			if len(got) != 1 || got[0].Field != tc.want || got[0].Rule != "format" {
				t.Errorf("violations = %+v, want one %s/format", got, tc.want)
			}
		})
	}
}

func TestQuestionFilterRejectsTextThatPostgresCannotStore(t *testing.T) { // phase-2 fix 2
	cases := []struct {
		name   string
		filter QuestionFilter
		fields []string
	}{
		{"q with a NUL", QuestionFilter{Text: "vp\x00c"}, []string{"q"}},
		{"q with invalid UTF-8", QuestionFilter{Text: "vp\xffc"}, []string{"q"}},
		{"origin with a NUL", QuestionFilter{Origin: "Set\x00"}, []string{"origin"}},
		{"origin with invalid UTF-8", QuestionFilter{Origin: "\xc3\x28"}, []string{"origin"}},
		{"both at once", QuestionFilter{Text: "\x00", Origin: "\xff"}, []string{"q", "origin"}},
		{"alongside a bad difficulty", QuestionFilter{Text: "\x00", Difficulty: "nope"}, []string{"difficulty", "q"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, v := range tc.filter.Violations() {
				if v.Field != "difficulty" && v.Rule != RuleInvalidText {
					t.Errorf("violation %+v, want rule invalid_text", v)
				}
				got = append(got, v.Field)
			}
			slices.Sort(got)
			want := slices.Clone(tc.fields)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("fields = %v, want %v", got, want)
			}
		})
	}
}

func TestQuestionFilterAcceptsOrdinaryUnicodeAndControlCharacters(t *testing.T) { // phase-2 fix 2
	f := QuestionFilter{Text: "ñandú \U0001F600 tab\there", Origin: "Set Ünï"}

	if got := f.Violations(); len(got) != 0 {
		t.Errorf("violations = %+v, want none", got)
	}
}
