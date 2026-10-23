package domain

// WeightMode says where the primary weights come from (Spec 18 FR-3.4).
type WeightMode string

const (
	WeightInferred WeightMode = "inferred"
	WeightManual   WeightMode = "manual"
)

const (
	// PositionPrimary marks the primary classification. Any other alive
	// classification has PositionSecondary; while an exam has no primary
	// (FR-3.1 rule 4), all of its classifications are secondary.
	PositionPrimary   = 0
	PositionSecondary = 1

	// MaxClassifications is BR-C1.
	MaxClassifications = 2
)

// Category is one value of a classification (Spec 18 §2). WeightPercent is
// meaningful on the primary classification only. QuestionCount is a read-model
// field: the alive questions linked to the category.
type Category struct {
	ID               string
	ClassificationID string
	Key              string // Slug(Name)
	Name             string
	Fold             string // Fold(Name), unique within the classification
	WeightPercent    int
	PaceQuestions    *int
	PaceMinutes      *int
	QuestionCount    int
}

// Classification is a named axis of categories, at most 2 per exam (BR-C1).
type Classification struct {
	ID         string
	ExamID     string
	Name       string
	Fold       string // Fold(Name), unique within the exam
	Position   int
	WeightMode WeightMode
	Categories []Category
}

// IsPrimary reports whether this is the exam's primary classification.
func (c Classification) IsPrimary() bool { return c.Position == PositionPrimary }

// Bank is what an import needs to know about an exam: its alive
// classifications (with categories, in creation order) and its alive
// questions. Both lists are in creation order.
type Bank struct {
	Classifications []Classification
	Questions       []Question
}
