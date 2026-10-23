package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestFoldNormalizesCaseAccentsAndWhitespace(t *testing.T) { // S03-3
	cases := []struct{ name, in, want string }{
		{"inner and outer whitespace", "  Alto   Rendimiento ", "alto rendimiento"},
		{"accent", "Señales", "senales"},
		{"several accents", "Planificación Física", "planificacion fisica"},
		{"tabs and newlines collapse to one space", "a\t\n b", "a b"},
		{"compatibility form (NFKD): fullwidth letters", "ＡＢＣ", "abc"},
		{"decomposed input equals composed input", "Señales", "senales"},
		{"empty", "", ""},
		{"only spaces", "   ", ""},
		{"already folded", "amazon s3", "amazon s3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Fold(tc.in); got != tc.want {
				t.Errorf("Fold(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFoldIsIdempotent(t *testing.T) {
	for _, in := range []string{"  Alto   Rendimiento ", "Señales", "ＡＢＣ", "Ñandú  Zorro"} {
		once := Fold(in)
		if twice := Fold(once); twice != once {
			t.Errorf("Fold(Fold(%q)) = %q, want %q", in, twice, once)
		}
	}
}

func TestSlugTurnsEveryRunOfNonAlphanumericsIntoOneDash(t *testing.T) { // S03-3
	cases := []struct{ in, want string }{
		{"Amazon S3 / Glacier", "amazon-s3-glacier"},
		{"  Alto   Rendimiento ", "alto-rendimiento"},
		{"Señales", "senales"},
		{"--Route 53--", "route-53"},
		{"A!!!B", "a-b"},
		{"???", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := Slug(tc.in); got != tc.want {
			t.Errorf("Slug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestLevenshteinRunsOnFoldedNames(t *testing.T) { // FR-3 similarity warning, AC-3
	cases := []struct {
		a, b string
		want int
	}{
		{"Alto rendimeinto", "Alto rendimiento", 2}, // AC-3: two adjacent letters swapped
		{"Alto rendimiento", "Alto rendimiento", 0},
		{"ALTO  Rendimiento", "alto rendimiento", 0}, // case and spacing are folded away
		{"Señales", "Senales", 0},                     // accents are folded away
		{"kitten", "sitting", 3},
		{"", "abc", 3},
		{"abc", "", 3},
		{"", "", 0},
		{"SQS", "SNS", 1},
		{"Seguridad", "Costos", 7},
	}
	for _, tc := range cases {
		if got := Levenshtein(tc.a, tc.b); got != tc.want {
			t.Errorf("Levenshtein(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
		if got := Levenshtein(tc.b, tc.a); got != tc.want {
			t.Errorf("Levenshtein(%q, %q) = %d, want %d (symmetry)", tc.b, tc.a, got, tc.want)
		}
	}
}

func TestStemFingerprintIsTheSha256OfTheFoldedStem(t *testing.T) { // AC-9
	sum := sha256.Sum256([]byte("which route 53 setting?"))
	want := hex.EncodeToString(sum[:])

	if got := StemFingerprint("Which  Route 53\nsetting? "); got != want {
		t.Errorf("StemFingerprint = %q, want %q", got, want)
	}
	if len(want) != 64 {
		t.Fatalf("test bug: fingerprint length %d", len(want))
	}
}

func TestStemFingerprintIgnoresCaseAndWhitespaceButNotWords(t *testing.T) { // AC-9
	a := StemFingerprint("What is  TTL?")
	if b := StemFingerprint("  what is ttl? "); a != b {
		t.Errorf("same stem modulo case and whitespace: %q != %q", a, b)
	}
	if c := StemFingerprint("What is TTL!"); a == c {
		t.Error("different punctuation must give a different fingerprint")
	}
}
