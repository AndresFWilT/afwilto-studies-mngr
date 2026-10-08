package domain

import (
	"slices"
	"strings"
)

// MaxCategoryDepth bounds the category tree; a root has depth 1 (OQ-6).
const MaxCategoryDepth = 5

// CheckPlacement is the category hierarchy policy (BR-CAT3, BR-CAT4).
// chain is the alive ancestor chain root → new parent (empty = place at the
// root); height is the height of the subtree being placed (1 for a leaf or a
// new category). categoryID is "" for a category that does not exist yet.
func CheckPlacement(categoryID string, chain []string, height int) error {
	// ids compare case-insensitively: a UUID in another case is the same row.
	sameID := func(id string) bool { return strings.EqualFold(id, categoryID) }
	if categoryID != "" && slices.ContainsFunc(chain, sameID) {
		return SingleViolation("parentId", "cycle", "a category cannot be placed under itself or one of its descendants")
	}
	if len(chain)+height > MaxCategoryDepth {
		return SingleViolation("parentId", "max_depth", "the category tree is limited to 5 levels")
	}
	return nil
}
