package domain

import (
	"fmt"
	"strings"
)

// InferWeights gives each category its share of the classified questions,
// rounded with the largest-remainder method so the shares sum to 100
// (FR-3.4, AC-6). counts maps a category id to its alive questions; the result
// has one entry per category, in the order given, which breaks ties. With no
// classified question every weight is 0.
func InferWeights(categories []Category, counts map[string]int) []CategoryWeight {
	shares := make([]int, len(categories))
	for i, c := range categories {
		shares[i] = counts[c.ID]
	}
	quotas := LargestRemainder(100, shares)
	out := make([]CategoryWeight, len(categories))
	for i, c := range categories {
		out[i] = CategoryWeight{CategoryID: c.ID, Percent: quotas[i]}
	}
	return out
}

// WeightInput is one manual weight of a PATCH.
type WeightInput struct {
	CategoryID string
	Percent    int
}

// ValidateManualWeights checks BR-C3 against the alive categories of the
// primary: every category listed exactly once, each weight an integer >= 0,
// the total 100. A list that skips a category is reported with the ids it
// skipped and not judged on its total.
func ValidateManualWeights(primary []Category, weights []WeightInput) []Violation {
	alive := map[string]bool{}
	for _, c := range primary {
		alive[c.ID] = true
	}
	var out []Violation
	listed := map[string]bool{}
	sum := 0
	for i, w := range weights {
		field := fmt.Sprintf("weights[%d]", i)
		switch {
		case !alive[w.CategoryID]:
			out = append(out, Violation{Field: field + ".categoryId", Rule: RuleNotFound,
				Message: fmt.Sprintf("category %s is not an alive category of the primary classification", w.CategoryID)})
		case listed[w.CategoryID]:
			out = append(out, Violation{Field: field + ".categoryId", Rule: "format",
				Message: fmt.Sprintf("category %s is listed more than once", w.CategoryID)})
		case w.Percent < 0 || w.Percent > 100:
			out = append(out, Violation{Field: field + ".percent", Rule: RuleRange,
				Message: fmt.Sprintf("a weight is a whole number from 0 to 100, got %d", w.Percent)})
		default:
			sum += w.Percent
		}
		listed[w.CategoryID] = true
	}

	var missing []string
	for _, c := range primary {
		if !listed[c.ID] {
			missing = append(missing, c.ID)
		}
	}
	switch {
	case len(missing) > 0:
		out = append(out, Violation{Field: "weights", Rule: RuleWeightsSum,
			Message: "every alive category of the primary classification needs a weight; missing: " + strings.Join(missing, ", ")})
	case len(out) == 0 && sum != 100:
		out = append(out, Violation{Field: "weights", Rule: RuleWeightsSum,
			Message: fmt.Sprintf("weights must sum to 100, they sum to %d", sum)})
	}
	return out
}

// refIndex resolves the name references of the questions to category ids.
type refIndex map[string]struct{ classificationID, categoryID string }

func (b Bank) refIndex() refIndex {
	idx := refIndex{}
	for _, c := range b.Classifications {
		for _, k := range c.Categories {
			idx[Fold(c.Name)+"\x00"+Fold(k.Name)] = struct{ classificationID, categoryID string }{c.ID, k.ID}
		}
	}
	return idx
}

// categoryIDs lists the distinct categories a question is linked to.
func (idx refIndex) categoryIDs(q Question) []string {
	var ids []string
	seen := map[string]bool{}
	for _, ref := range q.Categories {
		hit, ok := idx[Fold(ref.Classification)+"\x00"+Fold(ref.Category)]
		if ok && !seen[hit.categoryID] {
			seen[hit.categoryID] = true
			ids = append(ids, hit.categoryID)
		}
	}
	return ids
}

// LinkCounts maps each category id of the bank to the number of its alive
// questions, matching category references by folded names.
func (b Bank) LinkCounts() map[string]int { return b.countLinks("", "", "") }

// countLinks counts the questions per category as the bank would be without
// the question skipID, and with the links of category from moved to category
// into (a question linked to both counts once). Empty arguments do nothing.
func (b Bank) countLinks(skipID, from, into string) map[string]int {
	idx := b.refIndex()
	counts := map[string]int{}
	for _, q := range b.Questions {
		if q.ID == skipID && skipID != "" {
			continue
		}
		set := map[string]bool{}
		for _, id := range idx.categoryIDs(q) {
			if from != "" && id == from {
				id = into
			}
			set[id] = true
		}
		for id := range set {
			counts[id]++
		}
	}
	return counts
}

// primary returns the index of the primary classification, or -1.
func (b Bank) primary() int {
	for i, c := range b.Classifications {
		if c.IsPrimary() {
			return i
		}
	}
	return -1
}
