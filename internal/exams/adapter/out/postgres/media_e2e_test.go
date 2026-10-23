package postgres

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	imagepng "image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/afwilto/studies-mngr/api"
	examshttp "github.com/afwilto/studies-mngr/internal/exams/adapter/in/http"
	"github.com/afwilto/studies-mngr/internal/exams/adapter/out/fsstore"
	"github.com/afwilto/studies-mngr/internal/exams/app/usecase"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

// The real stack of slice 05: Postgres rows, files on disk, the HTTP adapter.

type e2e struct {
	*bankEnv
	dir    string
	router http.Handler
}

func newE2E(t *testing.T) *e2e {
	t.Helper()
	b, repo := newMediaEnv(t)
	dir := t.TempDir()
	store := fsstore.NewDiskFileStorage(dir)
	reader, err := examshttp.NewQuestionFileReader()
	if err != nil {
		t.Fatal(err)
	}
	deps := examshttp.Deps{
		ImportQuestions: usecase.NewImportQuestionsService(b.bank, realClock{}, uuidGen{}, usecase.WithMedia(repo, store)),
		FileReader:      reader,
		GetMedia:        usecase.NewGetMediaService(repo, store),
		ExportQuestions: usecase.NewExportQuestionsService(b.bank, repo),
	}
	router := httpx.NewServer(httpx.Config{BodyLimits: examshttp.BodyLimits(60 << 20)}, examshttp.Register(deps))
	return &e2e{bankEnv: b, dir: dir, router: router}
}

func samplePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := imagepng.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipOf(t *testing.T, files map[string][]byte, order ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range order {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write(files[name])
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func (e *e2e) upload(t *testing.T, query string, content []byte) (int, map[string]any) {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="upload"`)
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
	var out struct{ Data map[string]any }
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("response %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Code, out.Data
}

func (e *e2e) get(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func sha(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }

func filesOnDisk(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

func motoZip(t *testing.T) (zipBytes []byte, images [][]byte) {
	t.Helper()
	data, err := api.FS.ReadFile("examples/moto-images.json")
	if err != nil {
		t.Fatal(err)
	}
	images = [][]byte{samplePNG(t, 10, 10), samplePNG(t, 11, 10), samplePNG(t, 12, 10)}
	files := map[string][]byte{"questions.json": data, "img/entrada-prohibida.png": images[0], "img/ceda-el-paso.png": images[1], "img/stop.png": images[2]}
	return zipOf(t, files, "questions.json", "img/entrada-prohibida.png", "img/ceda-el-paso.png", "img/stop.png"), images
}

func TestMotoImagesZipIsStoredInPostgresAndOnDiskAndServed(t *testing.T) { // S05-1, AC-10
	e := newE2E(t)
	zipBytes, images := motoZip(t)

	status, r := e.upload(t, "", zipBytes)

	if status != http.StatusOK || r["applied"] != true {
		t.Fatalf("status %d report %v", status, r)
	}
	if n := mediaRows(t); n != 3 {
		t.Errorf("media rows = %d, want 3", n)
	}
	for _, img := range images {
		s := sha(img)
		var contentType string
		var bytesN, width int
		if err := pool.QueryRow(ctx, `SELECT content_type, bytes, width FROM media WHERE sha256 = $1`, s).Scan(&contentType, &bytesN, &width); err != nil {
			t.Fatalf("no row for %s: %v", s, err)
		}
		if contentType != "image/png" || bytesN != len(img) {
			t.Errorf("row = %s %d bytes", contentType, bytesN)
		}
		if _, err := os.Stat(filepath.Join(e.dir, s[:2], s[2:4], s)); err != nil {
			t.Errorf("file for %s: %v", s, err)
		}
		rec := e.get("/api/v1/media/" + s)
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), img) || rec.Header().Get("Content-Type") != "image/png" ||
			rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("GET /media/%s = %d %v", s, rec.Code, rec.Header())
		}
	}
	var stem string
	if err := pool.QueryRow(ctx, `SELECT stem FROM questions WHERE exam_id = $1 AND key = 'example-moto-0001'`, e.exam.ID).Scan(&stem); err != nil {
		t.Fatal(err)
	}
	var opt string
	if err := pool.QueryRow(ctx, `SELECT o.image FROM question_options o JOIN questions q ON q.id = o.question_id WHERE q.key = 'example-moto-0002' AND o.position = 0`).Scan(&opt); err != nil {
		t.Fatal(err)
	}
	ref := regexp.MustCompile(`media:[0-9a-f]{64}`)
	if !ref.MatchString(stem) || strings.Contains(stem, "img/") || !regexp.MustCompile(`^media:[0-9a-f]{64}$`).MatchString(opt) {
		t.Errorf("stored stem %q, option image %q", stem, opt)
	}
}

func TestAnImageUsedByFortyQuestionsIsOneRowAndOneFile(t *testing.T) { // AC-11
	e := newE2E(t)
	img := samplePNG(t, 8, 8)
	var qs []string
	for i := 0; i < 40; i++ {
		qs = append(qs, fmt.Sprintf(`{"key":"q%02d","type":"single","stem":"![x](media:img/shared.png)","options":[{"text":"a","correct":true},{"text":"b","correct":false}]}`, i))
	}
	doc := `{"format":"afwilto-questions","formatVersion":1,"questions":[` + strings.Join(qs, ",") + `]}`

	status, r := e.upload(t, "", zipOf(t, map[string][]byte{"questions.json": []byte(doc), "img/shared.png": img}, "questions.json", "img/shared.png"))

	if status != http.StatusOK || r["applied"] != true {
		t.Fatalf("status %d report %v", status, r)
	}
	if n := mediaRows(t); n != 1 {
		t.Errorf("media rows = %d, want 1", n)
	}
	if files := filesOnDisk(t, e.dir); len(files) != 1 {
		t.Errorf("files = %v, want 1", files)
	}
}

func TestExportThenReimportIsUnchangedAgainstPostgres(t *testing.T) { // S05-3
	e := newE2E(t)
	zipBytes, _ := motoZip(t)
	e.upload(t, "", zipBytes)
	exported := e.get("/api/v1/exams/" + e.exam.ID + "/questions/export")
	if exported.Code != http.StatusOK {
		t.Fatalf("export: %d %s", exported.Code, exported.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(exported.Body.Bytes()), int64(exported.Body.Len()))
	if err != nil || len(zr.File) != 4 {
		t.Fatalf("export zip: %v, %d entries", err, len(zr.File))
	}
	for _, f := range zr.File {
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		if strings.HasPrefix(f.Name, "img/") && f.Name != "img/"+sha(b)+".png" {
			t.Errorf("entry %s is not named after its own hash", f.Name)
		}
	}

	status, dry := e.upload(t, "dryRun=true", exported.Body.Bytes())
	if status != http.StatusOK {
		t.Fatalf("dry run: %d %v", status, dry)
	}
	c := dry["counts"].(map[string]any)
	media := dry["media"].(map[string]any)
	if c["unchanged"] != float64(2) || c["create"] != float64(0) || c["update"] != float64(0) || len(dry["newCategories"].([]any)) != 0 || len(media["missing"].([]any)) != 0 {
		t.Errorf("dry run = %v", dry)
	}

	before := examsChecksum(t)
	status, applied := e.upload(t, "", exported.Body.Bytes())
	if status != http.StatusOK || applied["counts"].(map[string]any)["unchanged"] != float64(2) {
		t.Fatalf("apply: %d %v", status, applied)
	}
	after := examsChecksum(t)
	for table, sum := range before {
		if after[table] != sum {
			t.Errorf("re-importing the export changed %s", table)
		}
	}
	if n := mediaRows(t); n != 3 {
		t.Errorf("media rows = %d, want still 3", n)
	}
}

func TestARejectedZipLeavesNoRowAndNoFile(t *testing.T) { // S05-2 "nothing is written", AC-13
	e := newE2E(t)
	doc := `{"format":"afwilto-questions","formatVersion":1,"questions":[{"key":"q","type":"single","stem":"![a](media:img/a.png) ![b](media:img/missing.png)","options":[{"text":"a","correct":true},{"text":"b","correct":false}]}]}`

	status, r := e.upload(t, "", zipOf(t, map[string][]byte{"questions.json": []byte(doc), "img/a.png": samplePNG(t, 5, 5)}, "questions.json", "img/a.png"))

	if status != http.StatusUnprocessableEntity || r["applied"] != false {
		t.Fatalf("status %d report %v", status, r)
	}
	if n := mediaRows(t); n != 0 || len(filesOnDisk(t, e.dir)) != 0 {
		t.Errorf("rows %d files %v; want none", n, filesOnDisk(t, e.dir))
	}
	var questions int
	pool.QueryRow(ctx, `SELECT count(*) FROM questions WHERE exam_id = $1`, e.exam.ID).Scan(&questions)
	if questions != 0 {
		t.Errorf("questions = %d", questions)
	}
}

func TestAnOrphanFileFromARolledBackImportIsReusedByTheNextOne(t *testing.T) { // slice 05 §1.4 "harmless because content-addressed"
	e := newE2E(t)
	img := samplePNG(t, 9, 9)
	s := sha(img)
	orphan := filepath.Join(e.dir, s[:2], s[2:4], s)
	if err := os.MkdirAll(filepath.Dir(orphan), 0o755); err != nil || os.WriteFile(orphan, img, 0o644) != nil {
		t.Fatal("cannot place the orphan")
	}
	doc := `{"format":"afwilto-questions","formatVersion":1,"questions":[{"key":"q","type":"single","stem":"![a](media:img/a.png)","options":[{"text":"a","correct":true},{"text":"b","correct":false}]}]}`

	status, r := e.upload(t, "", zipOf(t, map[string][]byte{"questions.json": []byte(doc), "img/a.png": img}, "questions.json", "img/a.png"))

	if status != http.StatusOK || r["applied"] != true || mediaRows(t) != 1 || len(filesOnDisk(t, e.dir)) != 1 {
		t.Errorf("status %d report %v rows %d files %v", status, r, mediaRows(t), filesOnDisk(t, e.dir))
	}
}
