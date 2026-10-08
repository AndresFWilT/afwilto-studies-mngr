package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func wantViolation(t *testing.T, err error, field, rule string) {
	t.Helper()
	var v *ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
	for _, viol := range v.Violations {
		if viol.Field == field && viol.Rule == rule {
			return
		}
	}
	t.Errorf("violations = %+v, want field %q rule %q", v.Violations, field, rule)
}

func TestNewCategoryName(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{"AWS", "AWS"},
		{"  Cloud Computing  ", "Cloud Computing"},
		{strings.Repeat("a", 60), strings.Repeat("a", 60)},
		{strings.Repeat("é", 60), strings.Repeat("é", 60)}, // runes, not bytes
	}
	for _, tc := range cases {
		got, err := NewCategoryName(tc.raw)
		if err != nil {
			t.Errorf("NewCategoryName(%q) error = %v", tc.raw, err)
			continue
		}
		if got.String() != tc.want {
			t.Errorf("NewCategoryName(%q) = %q, want %q", tc.raw, got.String(), tc.want)
		}
	}
}

func TestNewCategoryNameRejects(t *testing.T) {
	cases := map[string]string{
		"":                      "required",
		"   ":                   "required",
		strings.Repeat("a", 61): "max_length",
		"a\x00b":                "format",
		"line\nbreak":           "format",
		"tab\there":             "format",
		"del\x7f":               "format",
	}
	for raw, rule := range cases {
		_, err := NewCategoryName(raw)
		wantViolation(t, err, "name", rule)
	}
}

func TestCategoryNameKeyIsCaseInsensitive(t *testing.T) {
	a, _ := NewCategoryName("Cloud")
	b, _ := NewCategoryName(" cLOUD ")
	if a.Key() != b.Key() {
		t.Errorf("keys %q and %q differ, want equal", a.Key(), b.Key())
	}
}

func TestNewHexColor(t *testing.T) {
	for raw, want := range map[string]string{"#ff9900": "#FF9900", " #22d3ee ": "#22D3EE", "#ABCDEF": "#ABCDEF"} {
		got, err := NewHexColor(raw)
		if err != nil {
			t.Errorf("NewHexColor(%q) error = %v", raw, err)
			continue
		}
		if got.String() != want {
			t.Errorf("NewHexColor(%q) = %q, want %q", raw, got.String(), want)
		}
	}
	for _, raw := range []string{"", "ff9900", "#ff990", "#ff99001", "#gg9900", "blue"} {
		_, err := NewHexColor(raw)
		wantViolation(t, err, "color", "format")
	}
}

func TestCategoryDeleteSetsDeletedAt(t *testing.T) {
	name, _ := NewCategoryName("AWS")
	color, _ := NewHexColor("#FF9900")
	created := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	cat := NewCategory("id-1", name, color, nil, created)
	if cat.DeletedAt != nil {
		t.Fatal("new category already deleted")
	}

	later := created.Add(time.Hour)
	cat.Delete(later)

	if cat.DeletedAt == nil || !cat.DeletedAt.Equal(later) {
		t.Errorf("DeletedAt = %v, want %v", cat.DeletedAt, later)
	}
	if !cat.UpdatedAt.Equal(later) {
		t.Errorf("UpdatedAt = %v, want %v", cat.UpdatedAt, later)
	}
}
