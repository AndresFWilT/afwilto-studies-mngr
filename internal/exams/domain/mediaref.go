package domain

// MediaScheme prefixes every reference to an image in question text.
const MediaScheme = "media:"

// IsMediaSHA reports whether s is a stored-form media reference target: 64
// lowercase hexadecimal characters.
func IsMediaSHA(s string) bool { return false }

// MediaRefAt is one media reference found in a question: Path is its JSON path
// in the report's style ("questions[0].options[1].image"), Ref the target
// without the media: scheme.
type MediaRefAt struct{ Path, Ref string }

// MediaRefs lists the media references of the questions in document order.
func MediaRefs(qs []FileQuestion) []MediaRefAt { return nil }

// RewriteMediaMarkdown replaces the target of every Markdown media link in
// text with what fn returns for it; fn reports false to keep a reference.
func RewriteMediaMarkdown(text string, fn func(ref string) (string, bool)) string { return "" }

// RewriteFileMedia returns a copy of qs with every media reference rewritten
// by fn; qs is not modified.
func RewriteFileMedia(qs []FileQuestion, fn func(ref string) (string, bool)) []FileQuestion {
	return nil
}

// RewriteQuestionMedia returns a copy of q with every media reference
// rewritten by fn; q is not modified.
func RewriteQuestionMedia(q Question, fn func(ref string) (string, bool)) Question {
	return Question{}
}

// MediaExtension is the file extension (without dot) of a stored content type.
func MediaExtension(contentType string) (string, bool) { return "", false }
