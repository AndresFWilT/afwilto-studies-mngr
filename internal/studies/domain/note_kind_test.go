package domain

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestNewNoteKindName(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		want     string
		wantRule string
	}{
		{"trims", "  Lab report  ", "Lab report", ""},
		{"exactly 40 runes", strings.Repeat("ñ", 40), strings.Repeat("ñ", 40), ""},
		{"41 runes", strings.Repeat("ñ", 41), "", "max_length"},
		{"empty", "", "", "required"},
		{"blank", "   ", "", "required"},
		{"control char", "Lab\x00report", "", "format"},
		{"tab inside", "Lab\treport", "", "format"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewNoteKindName(tc.raw)
			if tc.wantRule == "" {
				if err != nil || got.String() != tc.want {
					t.Fatalf("got (%q, %v), want %q", got.String(), err, tc.want)
				}
				return
			}
			var v *ValidationError
			if !errors.As(err, &v) || v.Violations[0].Field != "name" || v.Violations[0].Rule != tc.wantRule {
				t.Fatalf("err = %v, want name/%s violation", err, tc.wantRule)
			}
		})
	}
}

func TestNoteKindNameKeyIsCaseFolded(t *testing.T) {
	a, _ := NewNoteKindName("Cheat Sheet")
	b, _ := NewNoteKindName("cheat sheet")
	if a.Key() != b.Key() {
		t.Fatalf("keys differ: %q vs %q", a.Key(), b.Key())
	}
}

func TestNoteKindDeleteIsLogical(t *testing.T) {
	name, _ := NewNoteKindName("Notes")
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	k := NewNoteKind("id", name, now)
	k.Delete(now.Add(time.Hour))
	if k.DeletedAt == nil || !k.DeletedAt.Equal(now.Add(time.Hour)) || !k.UpdatedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("Delete did not stamp deletedAt/updatedAt: %+v", k)
	}
}
