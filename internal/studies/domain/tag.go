package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

const (
	maxTagLen       = 40
	maxTagsPerNote  = 10
	maxLabelRunes   = 80
	maxLinksPerNote = 10
)

var (
	tagPattern      = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)
	slabPathPattern = regexp.MustCompile(`^/slab(/[a-z0-9-]+)+$`)
)

// normalizeTag applies the FR-5 normalization (trim, lowercase, whitespace
// runs → one '-') and validates the result. rule is empty when the tag is
// valid.
func normalizeTag(raw string) (tag, rule string) {
	tag = strings.ToLower(strings.Join(strings.Fields(raw), "-"))
	switch {
	case tag == "":
		return "", "required"
	case len([]rune(tag)) > maxTagLen:
		return "", "max_length"
	case !tagPattern.MatchString(tag):
		return "", "format"
	}
	return tag, ""
}

// NewTag normalizes and validates a single tag, for example from a query
// string. The violation is reported on field "tag".
func NewTag(raw string) (string, error) {
	tag, rule := normalizeTag(raw)
	if rule != "" {
		return "", SingleViolation("tag", rule, "tag must be lowercase kebab-case, 1–40 characters")
	}
	return tag, nil
}

// TagSet is a note's distinct, normalized tags, sorted by name. A set has no
// insertion order.
type TagSet struct{ names []string }

func NewTagSet(raw []string) (TagSet, error) {
	seen := make(map[string]struct{}, len(raw))
	names := make([]string, 0, len(raw))
	for i, r := range raw {
		tag, rule := normalizeTag(r)
		if rule != "" {
			return TagSet{}, SingleViolation(fmt.Sprintf("tags[%d]", i), rule,
				"tag must be lowercase kebab-case, 1–40 characters")
		}
		if _, dup := seen[tag]; !dup {
			seen[tag] = struct{}{}
			names = append(names, tag)
		}
	}
	if len(names) > maxTagsPerNote {
		return TagSet{}, SingleViolation("tags", "max_items", "a note has at most 10 distinct tags")
	}
	slices.Sort(names)
	return TagSet{names: names}, nil
}

// RehydrateTagSet trusts persisted data (already normalized).
func RehydrateTagSet(names []string) TagSet {
	out := slices.Clone(names)
	slices.Sort(out)
	return TagSet{names: out}
}

// Names returns a copy of the tags; never nil.
func (s TagSet) Names() []string {
	if len(s.names) == 0 {
		return []string{}
	}
	return slices.Clone(s.names)
}

// SlabLink points from a note to a slab page (Spec 16 FR-6).
type SlabLink struct {
	Label string
	Path  string
}

// SlabLinkInput is the unvalidated link as it arrives from a request.
type SlabLinkInput struct {
	Label string
	Path  string
}

// NewSlabLinks validates the whole list; order is preserved.
func NewSlabLinks(raw []SlabLinkInput) ([]SlabLink, error) {
	if len(raw) > maxLinksPerNote {
		return nil, SingleViolation("slabLinks", "max_items", "a note has at most 10 slab links")
	}
	links := make([]SlabLink, 0, len(raw))
	for i, r := range raw {
		label := strings.TrimSpace(r.Label)
		field := fmt.Sprintf("slabLinks[%d]", i)
		switch {
		case label == "":
			return nil, SingleViolation(field+".label", "required", "label must not be empty")
		case strings.ContainsFunc(label, unicode.IsControl):
			return nil, SingleViolation(field+".label", "format", "label must not contain control characters")
		case len([]rune(label)) > maxLabelRunes:
			return nil, SingleViolation(field+".label", "max_length", "label must be at most 80 characters")
		case !slabPathPattern.MatchString(r.Path):
			return nil, SingleViolation(field+".path", "format", "path must look like /slab/<segment>[/<segment>…] in lowercase kebab-case")
		}
		links = append(links, SlabLink{Label: label, Path: r.Path})
	}
	return links, nil
}
