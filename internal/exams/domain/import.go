package domain

import "time"

// ImportError is one failure of the import pipeline. Path is a JSON path in
// the report's style: "$", "questions[3].options", "questions[0].categories".
type ImportError struct {
	Path    string
	Rule    string
	Message string
}

type ImportCounts struct{ Create, Update, Unchanged, Invalid int }

type ImportKeys struct{ Create, Update, Unchanged []string }

// NewCategory is a category the import would create; SimilarTo lists existing
// categories of the same classification within Levenshtein distance 2 or with
// the same slug (FR-3.2).
type NewCategory struct {
	Classification string
	Name           string
	SimilarTo      []string
}

type WeightAfter struct {
	Category string
	Percent  int
	Mode     WeightMode
}

// DuplicateStem flags a created question whose stem fingerprint equals the
// one of an alive question with another key (AC-9).
type DuplicateStem struct{ Key, ExistingKey string }

type MediaReport struct {
	Referenced int
	InZip      int
	Missing    []string
	Unused     []string
}

// ImportReport is the answer of a dry run and of an apply (FR-4). Primary is
// nil when no primary classification could be resolved.
type ImportReport struct {
	DryRun             bool
	Applied            bool
	Counts             ImportCounts
	Keys               ImportKeys
	Errors             []ImportError
	Primary            *string
	NewClassifications []string
	NewCategories      []NewCategory
	WeightsAfter       []WeightAfter
	DuplicateStems     []DuplicateStem
	Media              MediaReport
	Preview            []Question
	Warnings           []string
}

// QuestionWrite is a question to persist and the categories it links to.
// ExpectedVersion is the stored version an update must still find.
type QuestionWrite struct {
	Question        Question
	CategoryIDs     []string
	ExpectedVersion int
}

// CategoryWeight is the weight to store on a primary category.
type CategoryWeight struct {
	CategoryID string
	Percent    int
}

// ImportWrite is everything an apply persists, in one transaction: first the
// classifications and categories, then the questions with their options and
// links, then the recomputed weights.
type ImportWrite struct {
	ExamID string
	Now    time.Time
	// Classifications and Categories are the new ones (FR-3).
	Classifications []Classification
	Categories      []Category
	// PromoteToPrimary is the id of an existing classification that becomes
	// the primary, or "".
	PromoteToPrimary string
	Creates          []QuestionWrite
	Updates          []QuestionWrite
	// Weights is set when the primary exists in inferred mode: one entry per
	// alive primary category.
	Weights []CategoryWeight
}

// PlanInput is everything PlanImport needs; it performs no I/O.
type PlanInput struct {
	ExamID string
	Bank   Bank
	File   QuestionFile
	// Primary is the `primary` query parameter, "" when absent.
	Primary string
	NewID   func() string
	Now     time.Time
}

// ImportPlan is the outcome of the semantic stages and of the inference. Write
// is nil exactly when Report.Errors is not empty.
type ImportPlan struct {
	Report ImportReport
	Write  *ImportWrite
}

// ClassificationUsage describes one classification for the primary
// resolution, in order of first appearance. Eligible means the default rule
// may pick it: every question of the file uses it with a single value.
type ClassificationUsage struct {
	Name     string
	Eligible bool
}

// PlanImport runs the semantic stages (3: BR-Q1/Q2/Q3, BR-C1, BR-C2; 4: media)
// and, when they pass, the inference and the diff against the bank.
func PlanImport(in PlanInput) ImportPlan { return ImportPlan{} }

// ResolvePrimary applies FR-3.1: the exam's existing primary, else the one
// named by the `primary` parameter (matched by Fold), else the first eligible
// classification. ok is false when none applies.
func ResolvePrimary(existing, param string, usages []ClassificationUsage) (name string, ok bool) {
	return "", false
}

// SimilarNames lists the existing names that a new name resembles: folded
// Levenshtein distance <= 2 or an equal slug (FR-3.2).
func SimilarNames(name string, existing []string) []string { return nil }

// DefaultSource stands in when neither the question nor the file has one
// (Question.source is required by the contract).
var DefaultSource = Source{Kind: "other", Name: "unspecified"}

// After returns the bank as it will be once w is applied, without touching b.
func (b Bank) After(w ImportWrite) Bank { return Bank{} }
