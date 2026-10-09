package domain

import (
	"slices"
	"strings"
	"testing"
)

func TestNewTag(t *testing.T) {
	tests := []struct {
		name, raw, want, rule string
	}{
		{"trim and lower", " Cloud Security ", "cloud-security", ""},
		{"upper", "IAM", "iam", ""},
		{"whitespace run and tab", "a \t  b", "a-b", ""},
		{"digits", "s3", "s3", ""},
		{"40 chars", strings.Repeat("a", 40), strings.Repeat("a", 40), ""},
		{"41 chars", strings.Repeat("a", 41), "", "max_length"},
		{"empty", "", "", "required"},
		{"blank", "   ", "", "required"},
		{"underscore and bang", "bad_tag!", "", "format"},
		{"leading dash", "-a", "", "format"},
		{"trailing dash", "a-", "", "format"},
		{"double dash", "a--b", "", "format"},
		{"unicode letter", "ñandú", "", "format"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewTag(tc.raw)
			if tc.rule == "" {
				if err != nil || got != tc.want {
					t.Fatalf("got (%q, %v), want %q", got, err, tc.want)
				}
				return
			}
			wantViolation(t, err, "tag", tc.rule)
		})
	}
}

func TestNewTagSet(t *testing.T) {
	set, err := NewTagSet([]string{" Cloud Security ", "cloud-security", "IAM"})
	if err != nil || !slices.Equal(set.Names(), []string{"cloud-security", "iam"}) {
		t.Fatalf("got (%v, %v)", set.Names(), err)
	}
	if set, _ := NewTagSet(nil); set.Names() == nil || len(set.Names()) != 0 {
		t.Fatalf("empty set must be a non-nil empty slice, got %#v", set.Names())
	}
	sorted, _ := NewTagSet([]string{"zeta", "alpha", "Mid"})
	if !slices.Equal(sorted.Names(), []string{"alpha", "mid", "zeta"}) {
		t.Fatalf("not sorted: %v", sorted.Names())
	}
}

func TestNewTagSetViolations(t *testing.T) {
	_, err := NewTagSet([]string{"ok", "bad_tag!"})
	wantViolation(t, err, "tags[1]", "format")
	_, err = NewTagSet([]string{"ok", " "})
	wantViolation(t, err, "tags[1]", "required")

	eleven := make([]string, 11)
	for i := range eleven {
		eleven[i] = "t" + string(rune('a'+i))
	}
	_, err = NewTagSet(eleven)
	wantViolation(t, err, "tags", "max_items")

	// Eleven entries that normalize to ten distinct tags are fine.
	dup := append(slices.Clone(eleven[:10]), " "+strings.ToUpper(eleven[0]))
	if set, err := NewTagSet(dup); err != nil || len(set.Names()) != 10 {
		t.Fatalf("got (%v, %v)", set.Names(), err)
	}
}
