package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// QuestionType is single (exactly one correct option) or multi (BR-Q1).
type QuestionType string

const (
	QuestionSingle QuestionType = "single"
	QuestionMulti  QuestionType = "multi"
)

// Source says where a question comes from (FR-2). Kind is one of
// purchased, generated, own, other.
type Source struct {
	Kind string
	Name string
}

// Origin locates a question in its original set or note. Zero fields are
// absent; an Origin with every field zero is never stored.
type Origin struct {
	Set     string
	Number  int
	NoteRef string
}

// Option is one answer of a question; its position in the slice is its
// letter. Empty strings are absent values.
type Option struct {
	Text      string
	Image     string // media:<path in the zip> in a file, media:<sha256> once stored
	Alt       string
	Correct   bool
	Rationale string
}

// ClassifiedNames lists the category names a question has in one
// classification. The names are as written in the file.
type ClassifiedNames struct {
	Classification string
	Names          []string
}

// FileQuestion is a question as read from an import file (FR-2). Categories
// keeps the order of the JSON object, which decides the order of first
// appearance.
type FileQuestion struct {
	Key         string
	Type        QuestionType
	Stem        string
	Options     []Option
	Explanation string
	Difficulty  string
	Categories  []ClassifiedNames
	Origin      *Origin
	Source      *Source
}

// QuestionFile is a parsed import file; Source is the file-level default.
type QuestionFile struct {
	Source    *Source
	Questions []FileQuestion
}

// CategoryRef names one category of a stored question.
type CategoryRef struct {
	Classification string
	Category       string
}

// Question is a stored question (Spec 18 §2). Explanation and Difficulty are
// empty when absent; Origin is nil when absent.
type Question struct {
	ID          string
	ExamID      string
	Key         string
	Type        QuestionType
	Stem        string
	Explanation string
	Difficulty  string
	Source      Source
	Origin      *Origin
	Categories  []CategoryRef
	Options     []Option
	Fingerprint string
	Version     int
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

const (
	minOptions, maxOptions = 2, 6
	minMultiOptions        = 3
)

var keyPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`)

// MaxOriginNumber is the largest origin.number (phase-2 fix 1); the JSON Schema
// says the same in questions.schema.json.
const MaxOriginNumber = 1_000_000

// ValidateQuestion applies BR-Q1, BR-Q2 and the BR-Q3 key pattern to one
// question, so that a path that does not go through the JSON Schema is still
// guarded. Violation fields are relative to the question: "key", "options",
// "options[2]". correct_count is reported at most once per question.
func ValidateQuestion(q FileQuestion) []Violation {
	var out []Violation
	out = append(out, invalidTextViolations(q)...)
	if !keyPattern.MatchString(q.Key) {
		out = append(out, Violation{Field: "key", Rule: RulePattern,
			Message: "key must match ^[a-z0-9][a-z0-9._-]{0,99}$ (lowercase letters, digits, dot, underscore, dash)"})
	}
	if q.Type != QuestionSingle && q.Type != QuestionMulti {
		out = append(out, Violation{Field: "type", Rule: RuleSchema, Message: "type must be single or multi"})
	}
	if n := len(q.Options); n < minOptions || n > maxOptions {
		out = append(out, Violation{Field: "options", Rule: RuleSchema,
			Message: fmt.Sprintf("a question has 2 to 6 options, this one has %d", n)})
	} else if msg := correctCountProblem(q); msg != "" {
		out = append(out, Violation{Field: "options", Rule: RuleCorrectCount, Message: msg})
	}
	if q.Origin != nil && (q.Origin.Number < 0 || q.Origin.Number > MaxOriginNumber) {
		out = append(out, Violation{Field: "origin.number", Rule: RuleRange,
			Message: fmt.Sprintf("origin.number must be a whole number from 1 to %d, got %d", MaxOriginNumber, q.Origin.Number)})
	}
	for i, o := range q.Options {
		switch {
		case strings.TrimSpace(o.Text) == "" && o.Image == "":
			out = append(out, Violation{Field: fmt.Sprintf("options[%d]", i), Rule: RuleOptionContent,
				Message: "an option needs text or an image"})
		case o.Image != "" && strings.TrimSpace(o.Alt) == "":
			out = append(out, Violation{Field: fmt.Sprintf("options[%d]", i), Rule: RuleOptionContent,
				Message: "an option with an image needs alt text"})
		}
	}
	return out
}

// correctCountProblem is BR-Q1; "" when the question holds.
func correctCountProblem(q FileQuestion) string {
	correct := 0
	for _, o := range q.Options {
		if o.Correct {
			correct++
		}
	}
	switch q.Type {
	case QuestionSingle:
		if correct != 1 {
			return fmt.Sprintf("a single-answer question has exactly 1 correct option, this one has %d", correct)
		}
	case QuestionMulti:
		switch {
		case len(q.Options) < minMultiOptions:
			return fmt.Sprintf("a multi-answer question has 3 to 6 options, this one has %d", len(q.Options))
		case correct < 2:
			return fmt.Sprintf("a multi-answer question has at least 2 correct options, this one has %d", correct)
		case correct == len(q.Options):
			return "a multi-answer question needs at least 1 wrong option"
		}
	}
	return ""
}

// hasNUL reports U+0000, which Postgres text and jsonb cannot store.
func hasNUL(s string) bool { return strings.ContainsRune(s, 0) }

func invalidText(field string) Violation {
	return Violation{Field: field, Rule: RuleInvalidText, Message: "the text contains a NUL character (U+0000), which cannot be stored"}
}

// invalidTextViolations lists every string of the question that holds a NUL
// byte (phase-1 review H3), at that string's path relative to the question.
// A name in `categories` is reported at `categories`.
func invalidTextViolations(q FileQuestion) []Violation {
	var out []Violation
	check := func(field, value string) {
		if hasNUL(value) {
			out = append(out, invalidText(field))
		}
	}
	check("key", q.Key)
	check("type", string(q.Type))
	check("stem", q.Stem)
	check("explanation", q.Explanation)
	check("difficulty", q.Difficulty)
	for i, o := range q.Options {
		check(fmt.Sprintf("options[%d].text", i), o.Text)
		check(fmt.Sprintf("options[%d].image", i), o.Image)
		check(fmt.Sprintf("options[%d].alt", i), o.Alt)
		check(fmt.Sprintf("options[%d].rationale", i), o.Rationale)
	}
	for _, c := range q.Categories {
		bad := hasNUL(c.Classification)
		for _, n := range c.Names {
			bad = bad || hasNUL(n)
		}
		if bad {
			out = append(out, invalidText("categories"))
			break
		}
	}
	if q.Origin != nil {
		check("origin.set", q.Origin.Set)
		check("origin.noteRef", q.Origin.NoteRef)
	}
	if q.Source != nil {
		check("source.kind", q.Source.Kind)
		check("source.name", q.Source.Name)
	}
	return out
}
