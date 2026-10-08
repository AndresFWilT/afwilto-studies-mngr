package domain

import "testing"

// CheckPlacement(categoryID, chain, height): chain is the alive ancestor chain
// root → new parent (empty = root); height is the moved subtree's height
// (1 for a leaf or a brand-new category; categoryID "" for a new one).
func TestCheckPlacementAccepts(t *testing.T) {
	cases := []struct {
		name   string
		id     string
		chain  []string
		height int
	}{
		{"new root", "", nil, 1},
		{"new child at depth 5", "", []string{"a", "b", "c", "d"}, 1},
		{"move leaf to root", "x", nil, 1},
		{"move under sibling", "x", []string{"a", "s"}, 1},
		{"subtree exactly fills depth 5", "x", []string{"a", "b"}, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := CheckPlacement(tc.id, tc.chain, tc.height); err != nil {
				t.Errorf("err = %v, want nil", err)
			}
		})
	}
}

func TestCheckPlacementRejects(t *testing.T) {
	cases := []struct {
		name   string
		id     string
		chain  []string
		height int
		rule   string
	}{
		{"new child at depth 6", "", []string{"a", "b", "c", "d", "e"}, 1, "max_depth"},
		{"subtree of height 2 under depth 4", "x", []string{"a", "b", "c", "d"}, 2, "max_depth"},
		{"under itself", "x", []string{"a", "x"}, 1, "cycle"},
		{"under direct child", "x", []string{"a", "x", "child"}, 2, "cycle"},
		{"under deep descendant", "x", []string{"x", "c1", "c2", "c3"}, 4, "cycle"},
		{"ids differ only in case", "ABC-1", []string{"root", "abc-1"}, 1, "cycle"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wantViolation(t, CheckPlacement(tc.id, tc.chain, tc.height), "parentId", tc.rule)
		})
	}
}
