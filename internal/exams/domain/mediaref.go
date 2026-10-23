package domain

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// MediaScheme prefixes every reference to an image in question text.
const MediaScheme = "media:"

var mediaSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

// IsMediaSHA reports whether s is a stored-form media reference target: 64
// lowercase hexadecimal characters.
func IsMediaSHA(s string) bool { return mediaSHA.MatchString(s) }

// markdownMedia matches the target of a Markdown link or image whose
// destination uses the media scheme: "](media:img/a.png", "](<media:...>" and
// "](  media:...". Group 1 is everything up to the target, group 2 the target
// (no whitespace, ")" or ">"), so a title after the target is left alone.
var markdownMedia = regexp.MustCompile(`(\]\(\s*<?media:)([^)\s>]+)`)

// MediaRefAt is one media reference found in a question: Path is its JSON path
// in the report's style ("questions[0].options[1].image"), Ref the target
// without the media: scheme.
type MediaRefAt struct{ Path, Ref string }

// MediaRefs lists the media references of the questions in document order:
// stem, explanation, then each option's text, image and rationale.
func MediaRefs(qs []FileQuestion) []MediaRefAt {
	var refs []MediaRefAt
	for i, q := range qs {
		visitMediaFields(q.Stem, q.Explanation, q.Options, func(field, value string, isImage bool) {
			if isImage {
				if ref, ok := strings.CutPrefix(value, MediaScheme); ok {
					refs = append(refs, MediaRefAt{Path: qPath(i, field), Ref: ref})
				}
				return
			}
			for _, m := range markdownMedia.FindAllStringSubmatch(value, -1) {
				refs = append(refs, MediaRefAt{Path: qPath(i, field), Ref: m[2]})
			}
		})
	}
	return refs
}

// visitMediaFields calls visit for every text of a question that can hold a
// media reference, in document order. isImage marks option.image, whose whole
// value is the reference; the other fields are Markdown.
func visitMediaFields(stem, explanation string, options []Option, visit func(field, value string, isImage bool)) {
	visit("stem", stem, false)
	visit("explanation", explanation, false)
	for j, o := range options {
		visit(fmt.Sprintf("options[%d].text", j), o.Text, false)
		visit(fmt.Sprintf("options[%d].image", j), o.Image, true)
		visit(fmt.Sprintf("options[%d].rationale", j), o.Rationale, false)
	}
}

// RewriteMediaMarkdown replaces the target of every Markdown media link in
// text with what fn returns for it; fn reports false to keep a reference.
func RewriteMediaMarkdown(text string, fn func(ref string) (string, bool)) string {
	if !strings.Contains(text, MediaScheme) {
		return text
	}
	return markdownMedia.ReplaceAllStringFunc(text, func(match string) string {
		parts := markdownMedia.FindStringSubmatch(match)
		if replacement, ok := fn(parts[2]); ok {
			return parts[1] + replacement
		}
		return match
	})
}

func rewriteImage(image string, fn func(string) (string, bool)) string {
	if ref, ok := strings.CutPrefix(image, MediaScheme); ok {
		if replacement, ok := fn(ref); ok {
			return MediaScheme + replacement
		}
	}
	return image
}

func rewriteMediaContent(stem, explanation string, options []Option, fn func(string) (string, bool)) (string, string, []Option) {
	out := slices.Clone(options)
	for i := range out {
		out[i].Text = RewriteMediaMarkdown(out[i].Text, fn)
		out[i].Image = rewriteImage(out[i].Image, fn)
		out[i].Rationale = RewriteMediaMarkdown(out[i].Rationale, fn)
	}
	return RewriteMediaMarkdown(stem, fn), RewriteMediaMarkdown(explanation, fn), out
}

// RewriteFileMedia returns a copy of qs with every media reference rewritten
// by fn; qs is not modified.
func RewriteFileMedia(qs []FileQuestion, fn func(ref string) (string, bool)) []FileQuestion {
	out := make([]FileQuestion, len(qs))
	for i, q := range qs {
		q.Stem, q.Explanation, q.Options = rewriteMediaContent(q.Stem, q.Explanation, q.Options, fn)
		out[i] = q
	}
	return out
}

// RewriteQuestionMedia returns a copy of q with every media reference
// rewritten by fn; q is not modified.
func RewriteQuestionMedia(q Question, fn func(ref string) (string, bool)) Question {
	q.Stem, q.Explanation, q.Options = rewriteMediaContent(q.Stem, q.Explanation, q.Options, fn)
	return q
}

// MediaExtension is the file extension (without dot) of a stored content type.
func MediaExtension(contentType string) (string, bool) {
	switch contentType {
	case "image/png":
		return "png", true
	case "image/jpeg":
		return "jpg", true
	case "image/webp":
		return "webp", true
	}
	return "", false
}

// QuestionExport is what the export endpoint needs (FR-5): the alive
// questions of an exam in stored form, and the stored images they use, by hash.
type QuestionExport struct {
	Questions []Question
	Media     map[string]Media
}

// QuestionMediaHashes lists the distinct hashes the stored questions
// reference, in order of first use.
func QuestionMediaHashes(qs []Question) []string {
	var hashes []string
	seen := map[string]bool{}
	add := func(ref string) {
		if IsMediaSHA(ref) && !seen[ref] {
			seen[ref] = true
			hashes = append(hashes, ref)
		}
	}
	for _, q := range qs {
		visitMediaFields(q.Stem, q.Explanation, q.Options, func(_, value string, isImage bool) {
			if isImage {
				if ref, ok := strings.CutPrefix(value, MediaScheme); ok {
					add(ref)
				}
				return
			}
			for _, m := range markdownMedia.FindAllStringSubmatch(value, -1) {
				add(m[2])
			}
		})
	}
	return hashes
}
