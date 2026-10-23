package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Fold, Slug, Levenshtein and StemFingerprint are the name-matching helpers of
// the category inference (Spec 18 FR-3, slice 03 §1.4).

// accentStripper decomposes (NFKD) and drops the combining marks. Transformers
// keep state, so each call builds its own chain.
func accentStripper() transform.Transformer {
	return transform.Chain(norm.NFKD, runes.Remove(runes.In(unicode.Mn)))
}

// Fold is the comparison form of a name: NFKD with the combining marks removed,
// lower-cased, trimmed, inner whitespace collapsed to one space.
func Fold(name string) string {
	stripped, _, err := transform.String(accentStripper(), name)
	if err != nil { // the chain never fails on valid text; keep the input rather than lose it
		stripped = name
	}
	return strings.Join(strings.Fields(strings.ToLower(stripped)), " ")
}

// Slug is Fold with every run of characters outside [a-z0-9] turned into one
// "-" and the ends trimmed ("Amazon S3 / Glacier" → "amazon-s3-glacier").
func Slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range Fold(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			dash = false
			b.WriteRune(r)
			continue
		}
		dash = true
	}
	return b.String()
}

// Levenshtein is the edit distance (insert, delete, substitute; each cost 1)
// between the folded forms of a and b, counted in runes.
func Levenshtein(a, b string) int {
	ra, rb := []rune(Fold(a)), []rune(Fold(b))
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// StemFingerprint is the hex sha256 of the folded stem. Two stems that differ
// only in case, accents or whitespace share it, which feeds the duplicate-stem
// warning (AC-9).
func StemFingerprint(stem string) string {
	sum := sha256.Sum256([]byte(Fold(stem)))
	return hex.EncodeToString(sum[:])
}
