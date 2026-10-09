package domain

import (
	"strings"
	"testing"
	"time"
)

func TestNewNoteTitle(t *testing.T) {
	tests := []struct {
		name, raw, want, rule string
	}{
		{"trims", "  Storage ", "Storage", ""},
		{"200 runes", strings.Repeat("ñ", 200), strings.Repeat("ñ", 200), ""},
		{"201 runes", strings.Repeat("ñ", 201), "", "max_length"},
		{"empty", "", "", "required"},
		{"blank", "  ", "", "required"},
		{"control char", "a\x00b", "", "format"},
		{"newline", "a\nb", "", "format"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewNoteTitle(tc.raw)
			if tc.rule == "" {
				if err != nil || got.String() != tc.want {
					t.Fatalf("got (%q, %v), want %q", got.String(), err, tc.want)
				}
				return
			}
			wantViolation(t, err, "title", tc.rule)
		})
	}
}

func TestNoteTitleKeyIsCaseFolded(t *testing.T) {
	a, _ := NewNoteTitle("Storage")
	b, _ := NewNoteTitle("STORAGE")
	if a.Key() != b.Key() {
		t.Fatal("keys differ")
	}
}

func TestNewMarkdownContent(t *testing.T) {
	md := "# T\n\n$$E=mc^2$$\n\n```go\nfmt.Println(\"ñ\")\n```\r\n  trailing  \n"
	tests := []struct {
		name, raw, rule string
	}{
		{"empty ok", "", ""},
		{"verbatim markdown", md, ""},
		{"exactly 200000", strings.Repeat("a", 200000), ""},
		{"200000 multibyte runes", strings.Repeat("ñ", 200000), ""},
		{"200001", strings.Repeat("a", 200001), "max_length"},
		{"NUL", "a\x00b", "format"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewMarkdownContent(tc.raw)
			if tc.rule != "" {
				wantViolation(t, err, "content", tc.rule)
				return
			}
			if err != nil || got.String() != tc.raw {
				t.Fatalf("content altered or rejected: %v", err)
			}
		})
	}
}

func TestParseSortSpec(t *testing.T) {
	tests := []struct {
		sort, order string
		want        SortSpec
		field, rule string
	}{
		{"", "", SortSpec{Field: SortByTitle}, "", ""},
		{"title", "desc", SortSpec{Field: SortByTitle, Desc: true}, "", ""},
		{"createdAt", "asc", SortSpec{Field: SortByCreatedAt}, "", ""},
		{"updatedAt", "desc", SortSpec{Field: SortByUpdatedAt, Desc: true}, "", ""},
		{"size", "", SortSpec{}, "sort", "enum"},
		{"Title", "", SortSpec{}, "sort", "enum"},
		{"", "bogus", SortSpec{}, "order", "enum"},
	}
	for _, tc := range tests {
		got, err := ParseSortSpec(tc.sort, tc.order)
		if tc.rule != "" {
			wantViolation(t, err, tc.field, tc.rule)
			continue
		}
		if err != nil || got != tc.want {
			t.Errorf("ParseSortSpec(%q,%q) = (%+v, %v), want %+v", tc.sort, tc.order, got, err, tc.want)
		}
	}
}

func TestNoteVersioning(t *testing.T) {
	title, _ := NewNoteTitle("Storage")
	content, _ := NewMarkdownContent("x")
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	n := NewNote("id", "sub", "kind", title, content, now)
	if n.Version != 1 || !n.CreatedAt.Equal(now) || !n.UpdatedAt.Equal(now) {
		t.Fatalf("new note = %+v", n)
	}
	n.Touch(now.Add(time.Minute))
	if n.Version != 2 || !n.UpdatedAt.Equal(now.Add(time.Minute)) || !n.CreatedAt.Equal(now) {
		t.Fatalf("after Touch = %+v", n)
	}
	n.Delete(now.Add(time.Hour))
	if n.DeletedAt == nil || !n.DeletedAt.Equal(now.Add(time.Hour)) || n.Version != 2 {
		t.Fatalf("after Delete = %+v", n)
	}
}
