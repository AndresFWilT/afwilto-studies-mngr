package postgres

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"image"
	imagepng "image/png"
	"io/fs"
	"math/rand/v2"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// Phase-2 fix 9: "nothing is written" (S05-2) is proved against real Postgres
// and a real directory for every S05-2 fixture, not only for AC-13: no media
// row, no file, and every exams-context row unchanged, for the apply and for
// the dry run.

type rawEntry struct {
	name string
	data []byte
	mode fs.FileMode // 0 = a regular file
}

func rawZip(t *testing.T, entries ...rawEntry) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Store, Modified: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(e.data)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// declareSize rewrites the uncompressed size the central directory declares for name.
func declareSize(t *testing.T, data []byte, name string, size uint32) []byte {
	t.Helper()
	out := bytes.Clone(data)
	sig := []byte("PK\x01\x02")
	for i := 0; i+46 <= len(out); {
		j := bytes.Index(out[i:], sig)
		if j < 0 {
			break
		}
		i += j
		nameLen := int(binary.LittleEndian.Uint16(out[i+28:]))
		if i+46+nameLen <= len(out) && string(out[i+46:i+46+nameLen]) == name {
			binary.LittleEndian.PutUint32(out[i+24:], size)
			return out
		}
		i += 4
	}
	t.Fatalf("no central directory entry %q", name)
	return nil
}

func noisyPNGAtLeast(t *testing.T, n int) []byte {
	t.Helper()
	rng := rand.New(rand.NewPCG(1, 2))
	for side := 64; ; side *= 2 {
		img := image.NewNRGBA(image.Rect(0, 0, side, side))
		for i := range img.Pix {
			img.Pix[i] = byte(rng.UintN(256))
		}
		var buf bytes.Buffer
		if err := imagepng.Encode(&buf, img); err != nil {
			t.Fatal(err)
		}
		if buf.Len() >= n {
			return buf.Bytes()
		}
	}
}

func (e *e2e) post(t *testing.T, query string, content []byte) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="upload.zip"`)
	part, _ := w.CreatePart(h)
	part.Write(content)
	w.Close()
	path := "/api/v1/exams/" + e.exam.ID + "/questions/import"
	if query != "" {
		path += "?" + query
	}
	req := httptest.NewRequest(http.MethodPost, path, &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	return rec
}

func TestEveryRejectedS052FixtureLeavesNoRowNoFileAndNoChangeInPostgresAndOnDisk(t *testing.T) { // S05-2 "nothing is written", AC-12
	oneImage := func(ref string) rawEntry {
		return rawEntry{name: "questions.json", data: []byte(`{"format":"afwilto-questions","formatVersion":1,"questions":[{"key":"q","type":"single","stem":"![x](media:` + ref + `)","options":[{"text":"a","correct":true},{"text":"b","correct":false}]}]}`)}
	}
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	thousandAndOne := []rawEntry{oneImage("img/a.png")}
	for i := 0; i < 1000; i++ {
		thousandAndOne = append(thousandAndOne, rawEntry{name: fmt.Sprintf("img/%05d.png", i), data: []byte{byte(i)}})
	}
	tiny := func(t *testing.T) []byte { return samplePNG(t, 4, 4) }

	// zip-level fixtures answer an error envelope on `file`; image-level ones a report.
	type fixture struct {
		name     string
		zip      func(t *testing.T) []byte
		fileRule string // the rule of the `file` error, or ""
		stemRule string // the rule of the report error at questions[0].stem, or ""
	}
	fixtures := []fixture{
		{"1,001 entries", func(t *testing.T) []byte { return rawZip(t, thousandAndOne...) }, domain.RuleZipEntries, ""},
		{"an entry declaring 200 MiB", func(t *testing.T) []byte {
			return declareSize(t, rawZip(t, oneImage("img/big.png"), rawEntry{name: "img/big.png", data: []byte("x")}), "img/big.png", 200<<20)
		}, domain.RuleZipSize, ""},
		{"../x.png", func(t *testing.T) []byte {
			return rawZip(t, oneImage("img/a.png"), rawEntry{name: "../x.png", data: tiny(t)})
		}, domain.RuleZipPath, ""},
		{"/abs.png", func(t *testing.T) []byte {
			return rawZip(t, oneImage("img/a.png"), rawEntry{name: "/abs.png", data: tiny(t)})
		}, domain.RuleZipPath, ""},
		{"a symlink entry", func(t *testing.T) []byte {
			return rawZip(t, oneImage("img/a.png"), rawEntry{name: "img/a.png", data: []byte("/etc/passwd"), mode: fs.ModeSymlink | 0o777})
		}, domain.RuleZipPath, ""},
		{"two root JSON files", func(t *testing.T) []byte {
			return rawZip(t, oneImage("img/a.png"), rawEntry{name: "second.json", data: []byte("{}")}, rawEntry{name: "img/a.png", data: tiny(t)})
		}, domain.RuleZipLayout, ""},
		{"a 3 MiB PNG", func(t *testing.T) []byte {
			return rawZip(t, oneImage("img/pic.png"), rawEntry{name: "img/pic.png", data: noisyPNGAtLeast(t, 3<<20)})
		}, "", domain.RuleMediaSize},
		{"a 5000x10 PNG", func(t *testing.T) []byte {
			return rawZip(t, oneImage("img/pic.png"), rawEntry{name: "img/pic.png", data: samplePNG(t, 5000, 10)})
		}, "", domain.RuleMediaDimensions},
		{"an SVG named .png (AC-12)", func(t *testing.T) []byte {
			return rawZip(t, oneImage("img/pic.png"), rawEntry{name: "img/pic.png", data: svg})
		}, "", domain.RuleMediaType},
	}

	for _, fx := range fixtures {
		for _, query := range []string{"", "dryRun=true"} {
			t.Run(fx.name+"/dryRun="+fmt.Sprint(query != ""), func(t *testing.T) {
				e := newE2E(t)
				before := examsChecksum(t)

				rec := e.post(t, query, fx.zip(t))

				if rec.Code != http.StatusUnprocessableEntity {
					t.Fatalf("status %d, want 422; body %.300s", rec.Code, rec.Body.String())
				}
				var env struct {
					Error struct {
						Details []struct{ Field, Rule string }
					}
					Data struct {
						Applied bool
						Errors  []struct{ Path, Rule string }
					}
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
					t.Fatal(err)
				}
				switch {
				case fx.fileRule != "":
					if len(env.Error.Details) != 1 || env.Error.Details[0].Field != "file" || env.Error.Details[0].Rule != fx.fileRule {
						t.Errorf("details = %+v, want file/%s", env.Error.Details, fx.fileRule)
					}
				default:
					if env.Data.Applied || len(env.Data.Errors) != 1 || env.Data.Errors[0].Path != "questions[0].stem" || env.Data.Errors[0].Rule != fx.stemRule {
						t.Errorf("report = %+v, want one questions[0].stem/%s and not applied", env.Data, fx.stemRule)
					}
				}

				if n := mediaRows(t); n != 0 {
					t.Errorf("media rows = %d, want 0", n)
				}
				if files := filesOnDisk(t, e.dir); len(files) != 0 {
					t.Errorf("files on disk = %v, want none", files)
				}
				after := examsChecksum(t)
				for table, sum := range before {
					if after[table] != sum {
						t.Errorf("table %s changed", table)
					}
				}
			})
		}
	}
}
