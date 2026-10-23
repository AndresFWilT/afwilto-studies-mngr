package http

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

func pngOf(t testing.TB, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// noisyPNG is an incompressible PNG of at least n bytes.
func noisyPNG(t testing.TB, n int) []byte {
	t.Helper()
	rng := rand.New(rand.NewPCG(1, 2))
	side := 64
	for {
		img := image.NewNRGBA(image.Rect(0, 0, side, side))
		for i := range img.Pix {
			img.Pix[i] = byte(rng.UintN(256))
		}
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		if buf.Len() >= n {
			return buf.Bytes()
		}
		side *= 2
	}
}

// padTo appends zero bytes after the image data (decoders stop at the end of
// the image), to hit an exact size.
func padTo(data []byte, n int) []byte { return append(slicesClone(data), make([]byte, n-len(data))...) }

func slicesClone(b []byte) []byte { return append([]byte(nil), b...) }

func jpegOf(t testing.TB, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewGray(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// onePixelWebP is a real 1x1 lossless WebP.
func onePixelWebP(t testing.TB) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString("UklGRhoAAABXRUJQVlA4TA0AAAAvAAAAEAcQERGIiP4HAA==")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// webpHeader is a lossless WebP that holds only the VP8L header of a w x h
// image, enough for the image header to be read.
func webpHeader(w, h int) []byte {
	bits := uint32(w-1) | uint32(h-1)<<14
	chunk := []byte{0x2f, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(chunk[1:], bits)
	body := append([]byte("WEBPVP8L"), 5, 0, 0, 0)
	body = append(body, chunk...)
	body = append(body, 0) // pad byte of an odd-sized chunk
	out := append([]byte("RIFF"), 0, 0, 0, 0)
	binary.LittleEndian.PutUint32(out[4:], uint32(len(body)))
	return append(out, body...)
}

func sumHex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func wantProblem(t *testing.T, f domain.MediaFile, rule string) {
	t.Helper()
	if f.Problem == nil {
		t.Fatalf("accepted as %+v, want rule %s", f.Media, rule)
	}
	if f.Problem.Rule != rule || f.Problem.Message == "" {
		t.Errorf("problem = %+v, want rule %s with a message", f.Problem, rule)
	}
}

func TestInspectImageAcceptsPNGJPEGAndWebPAndReadsTheirHeaders(t *testing.T) { // FR-6, S05-1
	for name, tc := range map[string]struct {
		data        []byte
		contentType string
		w, h        int
	}{
		"png":           {pngOf(t, 10, 20), "image/png", 10, 20},
		"jpeg":          {jpegOf(t, 30, 15), "image/jpeg", 30, 15},
		"webp":          {onePixelWebP(t), "image/webp", 1, 1},
		"webp header":   {webpHeader(640, 480), "image/webp", 640, 480},
		"png at limit":  {pngOf(t, 4096, 1), "image/png", 4096, 1},
		"tall at limit": {pngOf(t, 1, 4096), "image/png", 1, 4096},
	} {
		t.Run(name, func(t *testing.T) {
			f := inspectImage("img/x.bin", tc.data)

			if f.Problem != nil {
				t.Fatalf("problem = %+v", f.Problem)
			}
			want := domain.Media{SHA256: sumHex(tc.data), ContentType: tc.contentType, Bytes: len(tc.data), Width: tc.w, Height: tc.h}
			if f.Media != want || f.Path != "img/x.bin" || !bytes.Equal(f.Data, tc.data) {
				t.Errorf("media = %+v (path %q), want %+v", f.Media, f.Path, want)
			}
		})
	}
}

func TestInspectImageDecidesByMagicBytesNotByTheName(t *testing.T) { // FR-6 "never from the extension"
	if f := inspectImage("img/actually-jpeg.png", jpegOf(t, 4, 4)); f.Problem != nil || f.Media.ContentType != "image/jpeg" {
		t.Errorf("jpeg named .png: %+v", f)
	}
	if f := inspectImage("img/a.svg", pngOf(t, 4, 4)); f.Problem != nil || f.Media.ContentType != "image/png" {
		t.Errorf("png named .svg: %+v", f)
	}
}

func TestInspectImageRejectsAnythingElseAsMediaType(t *testing.T) { // AC-12 a .png holding SVG, FR-6 SVG is rejected
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><script>alert(1)</script></svg>`)
	for name, data := range map[string][]byte{
		"svg":                      svg,
		"svg with xml prolog":      append([]byte(`<?xml version="1.0"?>`), svg...),
		"svg behind a BOM":         append([]byte{0xEF, 0xBB, 0xBF}, svg...),
		"html":                     []byte("<!doctype html><title>x</title>"),
		"gif":                      []byte("GIF89a\x01\x00\x01\x00\x80\x00\x00"),
		"bmp":                      []byte("BM\x1e\x00\x00\x00\x00\x00\x00\x00\x1a\x00\x00\x00"),
		"pdf":                      []byte("%PDF-1.7\n"),
		"plain text":               []byte("hello"),
		"empty":                    {},
		"png magic only":           []byte("\x89PNG\r\n\x1a\n"),
		"png header truncated":     pngOf(t, 10, 10)[:20],
		"jpeg magic and junk":      append([]byte{0xFF, 0xD8, 0xFF}, bytes.Repeat([]byte{1}, 40)...),
		"riff that is not webp":    []byte("RIFF\x24\x00\x00\x00WAVEfmt "),
		"webp magic and junk":      append([]byte("RIFF\x24\x00\x00\x00WEBP"), bytes.Repeat([]byte{1}, 40)...),
		"png with svg after magic": append([]byte("\x89PNG\r\n\x1a\n"), svg...),
	} {
		t.Run(name, func(t *testing.T) {
			wantProblem(t, inspectImage("img/pic.png", data), domain.RuleMediaType)
		})
	}
}

func TestInspectImageSizeLimitIsTwoMiBInclusive(t *testing.T) { // S05-2 a 3 MiB PNG, boundary
	base := pngOf(t, 8, 8)

	if f := inspectImage("a.png", padTo(base, 2<<20)); f.Problem != nil {
		t.Errorf("exactly 2 MiB rejected: %+v", f.Problem)
	}
	wantProblem(t, inspectImage("a.png", padTo(base, 2<<20+1)), domain.RuleMediaSize)
	wantProblem(t, inspectImage("a.png", noisyPNG(t, 3<<20)), domain.RuleMediaSize)
}

func TestInspectImageDimensionLimitIs4096PerSideInclusive(t *testing.T) { // S05-2 a 5000x10 PNG, boundary
	wantProblem(t, inspectImage("a.png", pngOf(t, 5000, 10)), domain.RuleMediaDimensions)
	wantProblem(t, inspectImage("a.png", pngOf(t, 4097, 1)), domain.RuleMediaDimensions)
	wantProblem(t, inspectImage("a.png", pngOf(t, 1, 4097)), domain.RuleMediaDimensions)
	wantProblem(t, inspectImage("a.webp", webpHeader(5000, 10)), domain.RuleMediaDimensions)
}

func TestInspectImageProblemMessagesNameTheEntry(t *testing.T) {
	f := inspectImage("img/bad name.png", []byte("hello"))

	if f.Problem == nil || !strings.Contains(f.Problem.Message, "img/bad name.png") {
		t.Errorf("problem = %+v, want a message naming the entry", f.Problem)
	}
}

// ── archiveForQuestions ─────────────────────────────────────────────────────

func questionWith(refs ...string) []domain.FileQuestion {
	q := domain.FileQuestion{Key: "q", Type: domain.QuestionSingle, Stem: "s", Options: []domain.Option{{Text: "a", Correct: true}, {Text: "b"}}}
	for _, r := range refs {
		q.Stem += " ![x](media:" + r + ")"
	}
	return []domain.FileQuestion{q}
}

func mustArchive(t *testing.T, z *importZip, qs []domain.FileQuestion) *domain.MediaArchive {
	t.Helper()
	a, err := archiveForQuestions(z, qs)
	if err != nil || a == nil {
		t.Fatalf("archiveForQuestions = %v, %v", a, err)
	}
	return a
}

func TestArchiveForQuestionsInspectsOnlyTheEntriesTheQuestionsReference(t *testing.T) { // slice 05 §1.2 last bullet
	good := pngOf(t, 5, 5)
	data := buildZip(t, questionsEntry(), zent{name: "img/used.png", data: good}, zent{name: "img/unused.png", data: bytes.Repeat([]byte("zzzz"), 400)})
	z, err := openImportZip(corruptBody(t, data, "img/unused.png"), defaultZipLimits())
	if err != nil {
		t.Fatal(err)
	}

	a := mustArchive(t, z, questionWith("img/used.png", "img/used.png", "img/missing.png", strings.Repeat("a", 64)))

	if want := []string{"img/unused.png", "img/used.png"}; strings.Join(a.Names, ",") != strings.Join(want, ",") {
		t.Errorf("Names = %v, want %v", a.Names, want)
	}
	if len(a.Files) != 1 || a.Files["img/used.png"].Media.SHA256 != sumHex(good) {
		t.Errorf("Files = %v, want only img/used.png inspected", a.Files)
	}
}

func TestArchiveForQuestionsFlagsAnOversizedEntryFromItsHeaderAsMediaSize(t *testing.T) {
	big := noisyPNG(t, 3<<20)
	z, err := openImportZip(buildZip(t, questionsEntry(), zent{name: "img/big.png", data: big, store: true}), defaultZipLimits())
	if err != nil {
		t.Fatal(err)
	}

	a := mustArchive(t, z, questionWith("img/big.png"))

	wantProblem(t, a.Files["img/big.png"], domain.RuleMediaSize)
}

func TestArchiveForQuestionsTurnsAnUnreadableEntryIntoAMediaTypeProblem(t *testing.T) {
	data := corruptBody(t, buildZip(t, questionsEntry(), zent{name: "img/a.png", data: bytes.Repeat([]byte("abcdefgh"), 500)}), "img/a.png")
	z, err := openImportZip(data, defaultZipLimits())
	if err != nil {
		t.Fatal(err)
	}

	a := mustArchive(t, z, questionWith("img/a.png"))

	wantProblem(t, a.Files["img/a.png"], domain.RuleMediaType)
}

func TestArchiveForQuestionsFailsTheWholeImportWhenAnEntryLiesAboutItsSize(t *testing.T) { // zip_size is a property of the archive
	data := patchDeclaredSize(t, buildZip(t, questionsEntry(), zent{name: "img/a.png", data: bytes.Repeat([]byte("q"), 4000)}), "img/a.png", 10)
	z, err := openImportZip(data, defaultZipLimits())
	if err != nil {
		t.Fatal(err)
	}

	_, err = archiveForQuestions(z, questionWith("img/a.png"))

	var v *domain.ValidationError
	if !errors.As(err, &v) || v.Violations[0].Rule != domain.RuleZipSize {
		t.Errorf("err = %v, want a zip_size validation error", err)
	}
}
