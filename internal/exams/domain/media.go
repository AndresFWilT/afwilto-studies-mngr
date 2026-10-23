package domain

import "time"

// Media is one stored image (Spec 18 §2): content-addressed, so SHA256 is its
// identity and two uploads of the same bytes are one Media.
type Media struct {
	SHA256      string
	ContentType string // image/png, image/jpeg or image/webp
	Bytes       int
	Width       int
	Height      int
	CreatedAt   time.Time
}

// MediaProblem is why an image of a zip cannot be imported: Rule is one of
// media_type, media_size or media_dimensions.
type MediaProblem struct{ Rule, Message string }

// MediaFile is an image entry of an import zip after the adapter inspected it.
// Problem is nil for a usable image; then Media and Data are set.
type MediaFile struct {
	Path    string
	Media   Media
	Data    []byte
	Problem *MediaProblem
}

// MediaArchive is what an import zip contributes to the plan. Names lists
// every file entry other than the root JSON (images or not, referenced or
// not); Files holds the inspection result of the entries the questions
// reference, by path.
type MediaArchive struct {
	Names []string
	Files map[string]MediaFile
}
