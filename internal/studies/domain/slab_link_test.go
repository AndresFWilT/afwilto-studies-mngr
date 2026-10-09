package domain

import (
	"strings"
	"testing"
)

func TestNewSlabLinks(t *testing.T) {
	ok, err := NewSlabLinks([]SlabLinkInput{
		{Label: " B-Tree lab ", Path: "/slab/ud/operative-systems"},
		{Label: "RR", Path: "/slab/ud/operative-systems/round-robin"},
		{Label: "RR", Path: "/slab/ud/operative-systems/round-robin"}, // duplicates allowed
	})
	if err != nil || len(ok) != 3 || ok[0].Label != "B-Tree lab" || ok[1].Path != "/slab/ud/operative-systems/round-robin" {
		t.Fatalf("got (%+v, %v)", ok, err)
	}
	if links, _ := NewSlabLinks(nil); links == nil || len(links) != 0 {
		t.Fatalf("empty must be non-nil empty, got %#v", links)
	}
}

func TestNewSlabLinksPathTable(t *testing.T) {
	tests := []struct {
		path string
		ok   bool
	}{
		{"/slab/ud/operative-systems", true},
		{"/slab/a", true},
		{"/slab/a1-b2/c3", true},
		{"/finance", false},
		{"https://x.com/slab/a", false},
		{"/slab/", false},
		{"/slab", false},
		{"/slab/UPPER", false},
		{"/slab/a_b", false},
		{"/slab/a//b", false},
		{"/slab/a/", false},
		{"slab/a", false},
		{"", false},
	}
	for _, tc := range tests {
		_, err := NewSlabLinks([]SlabLinkInput{{Label: "l", Path: tc.path}})
		if tc.ok && err != nil {
			t.Errorf("%q rejected: %v", tc.path, err)
		}
		if !tc.ok {
			wantViolation(t, err, "slabLinks[0].path", "format")
		}
	}
}

func TestNewSlabLinksLabelAndCount(t *testing.T) {
	_, err := NewSlabLinks([]SlabLinkInput{{Label: "  ", Path: "/slab/a"}})
	wantViolation(t, err, "slabLinks[0].label", "required")
	_, err = NewSlabLinks([]SlabLinkInput{{Label: strings.Repeat("ñ", 81), Path: "/slab/a"}})
	wantViolation(t, err, "slabLinks[0].label", "max_length")
	if _, err = NewSlabLinks([]SlabLinkInput{{Label: strings.Repeat("ñ", 80), Path: "/slab/a"}}); err != nil {
		t.Fatalf("80 runes: %v", err)
	}
	_, err = NewSlabLinks([]SlabLinkInput{{Label: "a\nb", Path: "/slab/a"}})
	wantViolation(t, err, "slabLinks[0].label", "format")

	eleven := make([]SlabLinkInput, 11)
	for i := range eleven {
		eleven[i] = SlabLinkInput{Label: "l", Path: "/slab/a"}
	}
	_, err = NewSlabLinks(eleven)
	wantViolation(t, err, "slabLinks", "max_items")
}
