package http

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // registers the JPEG decoder for image.DecodeConfig
	_ "image/png"  // registers the PNG decoder for image.DecodeConfig

	_ "golang.org/x/image/webp" // registers the WebP decoder for image.DecodeConfig

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// FR-6 image limits.
const (
	maxImageBytes = 2 << 20
	maxImageSide  = 4096
)

// imageFormats are the only accepted images, by magic bytes. SVG is not among
// them and has no decoder registered: text that merely looks like an image by
// its name never gets this far.
var imageFormats = []struct {
	format, contentType string
	matches             func(data []byte) bool
}{
	{"png", "image/png", func(d []byte) bool { return bytes.HasPrefix(d, []byte("\x89PNG\r\n\x1a\n")) }},
	{"jpeg", "image/jpeg", func(d []byte) bool { return bytes.HasPrefix(d, []byte("\xff\xd8\xff")) }},
	{"webp", "image/webp", func(d []byte) bool {
		return len(d) >= 12 && bytes.Equal(d[:4], []byte("RIFF")) && bytes.Equal(d[8:12], []byte("WEBP"))
	}},
}

func problem(path, rule, format string, args ...any) domain.MediaFile {
	return domain.MediaFile{Path: path, Problem: &domain.MediaProblem{Rule: rule, Message: path + ": " + fmt.Sprintf(format, args...)}}
}

// inspectImage decides what an archive entry is from its bytes alone: the
// extension is never consulted. A usable image comes back with its hash,
// content type and dimensions; anything else carries the Problem. Dimensions
// come from the image header (image.DecodeConfig), so no pixel is decoded.
func inspectImage(path string, data []byte) domain.MediaFile {
	if len(data) > maxImageBytes {
		return problem(path, domain.RuleMediaSize, "%d bytes; an image may be at most 2 MiB", len(data))
	}
	var format, contentType string
	for _, f := range imageFormats {
		if f.matches(data) {
			format, contentType = f.format, f.contentType
			break
		}
	}
	if format == "" {
		return problem(path, domain.RuleMediaType, "not a PNG, JPEG or WebP image (SVG and every other type are refused)")
	}
	config, decoded, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || decoded != format || config.Width <= 0 || config.Height <= 0 {
		return problem(path, domain.RuleMediaType, "starts like a %s image but its header cannot be read", format)
	}
	if config.Width > maxImageSide || config.Height > maxImageSide {
		return problem(path, domain.RuleMediaDimensions, "%d x %d px; an image may be at most %d px per side", config.Width, config.Height, maxImageSide)
	}
	sum := sha256.Sum256(data)
	return domain.MediaFile{
		Path: path, Data: data,
		Media: domain.Media{SHA256: hex.EncodeToString(sum[:]), ContentType: contentType, Bytes: len(data), Width: config.Width, Height: config.Height},
	}
}

// archiveForQuestions reads and inspects the entries the questions reference
// by path. Entries nobody references are not read at all. An entry that lies
// about its size fails the whole import (the archive is hostile); any other
// trouble with one entry is that entry's problem.
func archiveForQuestions(z *importZip, qs []domain.FileQuestion) (*domain.MediaArchive, error) {
	names := map[string]bool{}
	for _, n := range z.Names() {
		names[n] = true
	}
	archive := &domain.MediaArchive{Names: z.Names(), Files: map[string]domain.MediaFile{}}
	for _, r := range domain.MediaRefs(qs) {
		if domain.IsMediaSHA(r.Ref) || !names[r.Ref] {
			continue
		}
		if _, done := archive.Files[r.Ref]; done {
			continue
		}
		data, exceeded, err := z.ReadEntry(r.Ref, maxImageBytes)
		var validation *domain.ValidationError
		switch {
		case exceeded:
			archive.Files[r.Ref] = problem(r.Ref, domain.RuleMediaSize, "larger than 2 MiB; an image may be at most 2 MiB")
		case errors.As(err, &validation) && validation.Violations[0].Rule == domain.RuleZipSize:
			return nil, err
		case err != nil:
			archive.Files[r.Ref] = problem(r.Ref, domain.RuleMediaType, "the entry could not be read from the zip")
		default:
			archive.Files[r.Ref] = inspectImage(r.Ref, data)
		}
	}
	return archive, nil
}
