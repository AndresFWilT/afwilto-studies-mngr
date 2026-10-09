package domain

import (
	"strings"
	"testing"
	"time"
)

func TestNewSubjectName(t *testing.T) {
	tests := []struct {
		name, raw, want, rule string
	}{
		{"trims", "  Networking ", "Networking", ""},
		{"120 runes", strings.Repeat("ñ", 120), strings.Repeat("ñ", 120), ""},
		{"121 runes", strings.Repeat("ñ", 121), "", "max_length"},
		{"empty", "", "", "required"},
		{"blank", " \t ", "", "required"},
		{"control char", "Net\x00working", "", "format"},
		{"newline inside", "Net\nworking", "", "format"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewSubjectName(tc.raw)
			if tc.rule == "" {
				if err != nil || got.String() != tc.want {
					t.Fatalf("got (%q, %v), want %q", got.String(), err, tc.want)
				}
				return
			}
			wantViolation(t, err, "name", tc.rule)
		})
	}
}

func TestSubjectNameKeyIsCaseFolded(t *testing.T) {
	a, _ := NewSubjectName("Networking")
	b, _ := NewSubjectName("NETWORKING")
	if a.Key() != b.Key() {
		t.Fatalf("keys differ: %q vs %q", a.Key(), b.Key())
	}
}

func TestNewSubjectDescription(t *testing.T) {
	tests := []struct {
		name    string
		raw     *string
		wantNil bool
		rule    string
	}{
		{"nil stays nil", nil, true, ""},
		{"empty becomes nil", ptrTo(""), true, ""},
		{"multi-line kept", ptrTo("line1\nline2\ttab"), false, ""},
		{"2000 runes", ptrTo(strings.Repeat("ñ", 2000)), false, ""},
		{"2001 runes", ptrTo(strings.Repeat("ñ", 2001)), false, "max_length"},
		{"NUL", ptrTo("a\x00b"), false, "format"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewSubjectDescription(tc.raw)
			if tc.rule != "" {
				wantViolation(t, err, "description", tc.rule)
				return
			}
			if err != nil || (got == nil) != tc.wantNil {
				t.Fatalf("got (%v, %v), wantNil=%v", got, err, tc.wantNil)
			}
			if !tc.wantNil && *got != *tc.raw {
				t.Fatalf("description altered: %q vs %q", *got, *tc.raw)
			}
		})
	}
}

func ptrTo(s string) *string { return &s }

func TestSubjectDeleteIsLogical(t *testing.T) {
	name, _ := NewSubjectName("Networking")
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	s := NewSubject("id", "cat", name, nil, now)
	s.Delete(now.Add(time.Hour))
	if s.DeletedAt == nil || !s.DeletedAt.Equal(now.Add(time.Hour)) || !s.UpdatedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("Delete did not stamp: %+v", s)
	}
}
