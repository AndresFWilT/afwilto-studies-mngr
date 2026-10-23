package domain

// InferWeights gives each category its share of the classified questions,
// rounded with the largest-remainder method (FR-3.4). counts maps a category
// id to its alive questions; the result has one entry per category, in order.
func InferWeights(categories []Category, counts map[string]int) []CategoryWeight { return nil }

// ValidateManualWeights checks BR-C3 against the alive categories of the
// primary: every category listed once, integers >= 0, summing to 100.
func ValidateManualWeights(primary []Category, weights []WeightInput) []Violation { return nil }

// LinkCounts maps each category id of the bank to the number of its alive
// questions, matching category references by folded names.
func (b Bank) LinkCounts() map[string]int { return nil }

// WeightInput is one manual weight of a PATCH.
type WeightInput struct {
	CategoryID string
	Percent    int
}
