package domain

import (
	"slices"
	"time"
)

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
	// Media lists the images of the import's zip that the questions
	// reference, once per hash: the files to store and the rows to insert
	// (a row that exists is left alone).
	Media []MediaFile
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
	// Archive is the zip the file came in, nil for a .json import. KnownMedia
	// holds the stored hashes among the file's media:<sha256> references.
	Archive    *MediaArchive
	KnownMedia map[string]bool
}

// ImportPlan is the outcome of the semantic stages and of the inference. Write
// is nil exactly when Report.Errors is not empty.
type ImportPlan struct {
	Report ImportReport
	Write  *ImportWrite
}

// ClassificationUsage describes one classification for the primary
// resolution, in order of first appearance. Eligible means the default rule
// may pick it: the file uses it and no question lists more than one value in it.
type ClassificationUsage struct {
	Name     string
	Eligible bool
}

// ResolvePrimary applies FR-3.1: the exam's existing primary, else the one
// named by the `primary` parameter (matched by Fold), else the first eligible
// classification. ok is false when none applies. A parameter that names no
// classification of usages is skipped; PlanImport reports it as an error when
// it matters.
func ResolvePrimary(existing, param string, usages []ClassificationUsage) (name string, ok bool) {
	if existing != "" {
		return existing, true
	}
	if param != "" {
		for _, u := range usages {
			if Fold(u.Name) == Fold(param) {
				return u.Name, true
			}
		}
	}
	for _, u := range usages {
		if u.Eligible {
			return u.Name, true
		}
	}
	return "", false
}

// SimilarNames lists the existing names that a new name resembles: folded
// Levenshtein distance <= 2 or an equal slug (FR-3.2).
func SimilarNames(name string, existing []string) []string {
	var out []string
	slug := Slug(name)
	for _, e := range existing {
		if Levenshtein(name, e) <= 2 || (slug != "" && slug == Slug(e)) {
			out = append(out, e)
		}
	}
	return out
}

// DefaultSource stands in when neither the question nor the file has one
// (Question.source is required by the contract).
var DefaultSource = Source{Kind: "other", Name: "unspecified"}

// After returns the bank as it will be once w is applied, without touching b.
func (b Bank) After(w ImportWrite) Bank {
	out := Bank{}
	for _, c := range b.Classifications {
		c.Categories = slices.Clone(c.Categories)
		out.Classifications = append(out.Classifications, c)
	}
	for _, c := range w.Classifications {
		c.Categories = nil
		out.Classifications = append(out.Classifications, c)
	}
	for i := range out.Classifications {
		if out.Classifications[i].ID == w.PromoteToPrimary && w.PromoteToPrimary != "" {
			out.Classifications[i].Position = PositionPrimary
		}
	}
	for _, cat := range w.Categories {
		for i := range out.Classifications {
			if out.Classifications[i].ID == cat.ClassificationID {
				out.Classifications[i].Categories = append(out.Classifications[i].Categories, cat)
			}
		}
	}
	for _, cw := range w.Weights {
		for i := range out.Classifications {
			for j := range out.Classifications[i].Categories {
				if out.Classifications[i].Categories[j].ID == cw.CategoryID {
					out.Classifications[i].Categories[j].WeightPercent = cw.Percent
				}
			}
		}
	}

	updated := map[string]Question{}
	for _, u := range w.Updates {
		updated[u.Question.ID] = u.Question
	}
	for _, q := range b.Questions {
		if nq, ok := updated[q.ID]; ok {
			q = nq
		}
		out.Questions = append(out.Questions, cloneQuestion(q))
	}
	for _, c := range w.Creates {
		out.Questions = append(out.Questions, cloneQuestion(c.Question))
	}
	return out
}

func cloneQuestion(q Question) Question {
	q.Options = slices.Clone(q.Options)
	q.Categories = slices.Clone(q.Categories)
	return q
}
