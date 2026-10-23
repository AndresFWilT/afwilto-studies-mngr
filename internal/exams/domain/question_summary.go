package domain

import (
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// QuestionSummary is a row of the bank browser (Spec 18 slice 04 §1.1).
type QuestionSummary struct {
	ID          string
	Key         string
	Type        QuestionType
	StemExcerpt string
	Categories  []CategoryRef
	Difficulty  string // "" when absent
	Origin      *Origin
	Version     int
	UpdatedAt   time.Time
}

// QuestionFilter holds the bank browser filters, combined with AND. An empty
// field is no filter. Unclassified keeps the questions with no category in the
// primary classification.
type QuestionFilter struct {
	CategoryID   string
	Origin       string
	Difficulty   string
	Text         string
	Unclassified bool
}

// Difficulties are the allowed values of a question's difficulty.
var Difficulties = []string{"easy", "medium", "hard"}

// Violations reports the filter values that are not allowed: an unknown
// difficulty, and q or origin text Postgres cannot compare (U+0000, invalid
// UTF-8; phase-2 fix 2).
func (f QuestionFilter) Violations() []Violation {
	var out []Violation
	if f.Difficulty != "" && !slices.Contains(Difficulties, f.Difficulty) {
		out = append(out, Violation{Field: "difficulty", Rule: "format", Message: "difficulty must be easy, medium or hard"})
	}
	for _, text := range []struct{ field, value string }{{"q", f.Text}, {"origin", f.Origin}} {
		if hasNUL(text.value) || !utf8.ValidString(text.value) {
			out = append(out, Violation{Field: text.field, Rule: RuleInvalidText,
				Message: text.field + " must be valid text without NUL characters (U+0000)"})
		}
	}
	return out
}

const excerptRunes = 200

var (
	reFenceLine  = regexp.MustCompile("(?m)^[ \t]*(?:```|~~~).*$")
	reImage      = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	reLink       = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reHeading    = regexp.MustCompile(`(?m)^[ \t]{0,3}#{1,6}[ \t]+`)
	reQuote      = regexp.MustCompile(`(?m)^[ \t]{0,3}(?:>[ \t]?)+`)
	reListMarker = regexp.MustCompile(`(?m)^[ \t]*(?:[-*+]|\d+[.)])[ \t]+`)
	reCode       = regexp.MustCompile("`([^`]*)`")
	reStrike     = regexp.MustCompile(`~~(.+?)~~`)
	reBoldStar   = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reBoldUnder  = regexp.MustCompile(`(^|[^\w])__(.+?)__([^\w]|$)`)
	reItalicStar = regexp.MustCompile(`\*(\S(?:[^*]*?\S)?)\*`)
	reItalicUnd  = regexp.MustCompile(`(^|[^\w])_(\S(?:[^_]*?\S)?)_([^\w]|$)`)
)

// StemExcerpt is the first 200 characters of a stem with Markdown syntax
// stripped: fences, heading, quote and list markers, emphasis, code spans and
// link syntax go (images and links keep their text), and runs of whitespace
// become one space. Underscores inside words stay.
func StemExcerpt(stem string) string {
	s := reFenceLine.ReplaceAllString(stem, "")
	s = reImage.ReplaceAllString(s, "$1")
	s = reLink.ReplaceAllString(s, "$1")
	s = reHeading.ReplaceAllString(s, "")
	s = reQuote.ReplaceAllString(s, "")
	s = reListMarker.ReplaceAllString(s, "")
	s = reCode.ReplaceAllString(s, "$1")
	s = reStrike.ReplaceAllString(s, "$1")
	s = reBoldStar.ReplaceAllString(s, "$1")
	for range 2 { // a closing boundary is consumed by the match, so adjacent spans need a second pass
		s = reBoldUnder.ReplaceAllString(s, "$1$2$3")
	}
	s = reItalicStar.ReplaceAllString(s, "$1")
	for range 2 {
		s = reItalicUnd.ReplaceAllString(s, "$1$2$3")
	}
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > excerptRunes {
		s = string(r[:excerptRunes])
	}
	return s
}

// SummarizeQuestion builds the browser row of a stored question.
func SummarizeQuestion(q Question) QuestionSummary {
	return QuestionSummary{
		ID: q.ID, Key: q.Key, Type: q.Type, StemExcerpt: StemExcerpt(q.Stem),
		Categories: slices.Clone(q.Categories), Difficulty: q.Difficulty, Origin: q.Origin,
		Version: q.Version, UpdatedAt: q.UpdatedAt,
	}
}
