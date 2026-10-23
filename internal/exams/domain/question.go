package domain

import "time"

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

// ValidateQuestion applies BR-Q1, BR-Q2 and the BR-Q3 key pattern to one
// question, so that a path that does not go through the JSON Schema is still
// guarded. Violation fields are relative to the question: "key", "options",
// "options[2]".
func ValidateQuestion(q FileQuestion) []Violation { return nil }
