package domain

import (
	"strings"
	"time"
	"unicode"
)

const maxCategoryNameRunes = 60

// CategoryName is trimmed and 1–60 runes long. Sibling uniqueness compares
// Key(), the case-folded form.
type CategoryName struct{ value string }

func NewCategoryName(raw string) (CategoryName, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return CategoryName{}, SingleViolation("name", "required", "name must not be empty")
	}
	if strings.ContainsFunc(trimmed, unicode.IsControl) {
		return CategoryName{}, SingleViolation("name", "format", "name must not contain control characters")
	}
	if len([]rune(trimmed)) > maxCategoryNameRunes {
		return CategoryName{}, SingleViolation("name", "max_length", "name must be at most 60 characters")
	}
	return CategoryName{value: trimmed}, nil
}

// RehydrateCategoryName trusts persisted data.
func RehydrateCategoryName(value string) CategoryName { return CategoryName{value: value} }

func (n CategoryName) String() string { return n.value }
func (n CategoryName) Key() string    { return strings.ToLower(n.value) }

// HexColor is a 6-digit hex color, normalized to uppercase.
type HexColor struct{ hex string }

func NewHexColor(raw string) (HexColor, error) {
	trimmed := strings.TrimSpace(raw)
	if !isHexColor(trimmed) {
		return HexColor{}, SingleViolation("color", "format", "color must be a 6-digit hex value like #22D3EE")
	}
	return HexColor{hex: strings.ToUpper(trimmed)}, nil
}

func isHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, r := range s[1:] {
		if (r < '0' || r > '9') && (r < 'A' || r > 'F') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

// RehydrateHexColor trusts persisted data.
func RehydrateHexColor(hex string) HexColor { return HexColor{hex: hex} }

func (c HexColor) String() string { return c.hex }

// Category is a node of the category tree (adjacency list). ParentID nil
// means a root category.
type Category struct {
	ID        string
	ParentID  *string
	Name      CategoryName
	Color     HexColor
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt *time.Time
}

func NewCategory(id string, name CategoryName, color HexColor, parentID *string, now time.Time) Category {
	return Category{ID: id, ParentID: parentID, Name: name, Color: color, CreatedAt: now, UpdatedAt: now}
}

// Delete is logical: the row stays, marked deleted (Spec 16 FR-0).
func (c *Category) Delete(now time.Time) {
	c.DeletedAt = &now
	c.UpdatedAt = now
}

// CategoryView is the read model: the category plus values derived from the
// tree (display path, depth) and from its dependents.
type CategoryView struct {
	Category
	Path         string
	Depth        int
	SubjectCount int
}
