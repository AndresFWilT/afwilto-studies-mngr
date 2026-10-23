package http

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/afwilto/studies-mngr/internal/exams/app/usecase"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

const zipCap = 60 << 20 // above the 50 MiB zip limit, as MAX_IMPORT_BYTES is in production

type zipEnv struct {
	importEnv
	media *testsupport.FakeMedia
}

func newZipEnv(t *testing.T, cap int64) zipEnv {
	t.Helper()
	subjects := testsupport.NewFakeSubjectDirectory(subjectA, subjectB)
	repo := testsupport.NewFakeExamRepo(subjects)
	bank := testsupport.NewFakeQuestionBank(repo)
	media := testsupport.NewFakeMedia()
	bank.AttachMedia(media)
	clock := &testsupport.StepClock{}
	ids := &testsupport.SeqIDGen{}
	deps := Deps{
		CreateExam:       usecase.NewCreateExamService(repo, subjects, clock, ids),
		ListSubjectExams: usecase.NewListSubjectExamsService(repo, subjects),
		GetExam:          usecase.NewGetExamService(repo),
		UpdateExam:       usecase.NewUpdateExamService(repo, bank, clock),
		DeleteExam:       usecase.NewDeleteExamService(repo, clock),
		ImportQuestions:  usecase.NewImportQuestionsService(bank, clock, ids, usecase.WithMedia(media, media)),
		FileReader:       sharedReader(t),
		GetQuestion:      usecase.NewGetQuestionService(bank),
		GetMedia:         usecase.NewGetMediaService(media, media),
		ExportQuestions:  usecase.NewExportQuestionsService(bank, media),
	}
	env := apiEnv{router: httpx.NewServer(httpx.Config{MaxBodyBytes: testMaxBodyBytes, BodyLimits: BodyLimits(cap)}, Register(deps))}
	exam := env.createExam(t, subjectA, "Motos")
	return zipEnv{importEnv: importEnv{apiEnv: env, bank: bank, examID: exam["id"].(string)}, media: media}
}

// motoImagesZip is the fixture of S05-1: api/examples/moto-images.json plus
// three generated PNGs.
func motoImagesZip(t *testing.T) []byte {
	t.Helper()
	return buildZip(t,
		zent{name: "questions.json", data: example(t, "moto-images.json")},
		zent{name: "img/entrada-prohibida.png", data: pngOf(t, 10, 10)},
		zent{name: "img/ceda-el-paso.png", data: pngOf(t, 11, 10)},
		zent{name: "img/stop.png", data: pngOf(t, 12, 10)},
	)
}

// importReport is report() that fails the test, instead of panicking, when the
// body is not an import report.
func importReport(t *testing.T, rec *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	r := report(t, rec, status)
	if _, ok := r["errors"].([]any); !ok {
		t.Fatalf("not an import report: %s", rec.Body.String())
	}
	return r
}

func (e zipEnv) get(t *testing.T, path string) *httptest.ResponseRecorder {
	t.Helper()
	return e.do(t, http.MethodGet, path, "")
}

func (e zipEnv) nothingWritten(t *testing.T) {
	t.Helper()
	if e.media.Puts != 0 || len(e.media.Files) != 0 || len(e.media.Rows) != 0 || e.bank.Applied != 0 {
		t.Errorf("puts %d, files %d, rows %d, imports applied %d; want nothing written", e.media.Puts, len(e.media.Files), len(e.media.Rows), e.bank.Applied)
	}
}

var storedRef = regexp.MustCompile(`media:([0-9a-f]{64})`)

func TestImportingTheMotoImagesZipStoresThreeImagesAndRewritesTheReferences(t *testing.T) { // S05-1, AC-10
	e := newZipEnv(t, zipCap)

	rec := e.post(t, importPath(e.examID, ""), "moto-images.zip", motoImagesZip(t))

	r := importReport(t, rec, http.StatusOK)
	if r["applied"] != true || counts(r) != [4]float64{2, 0, 0, 0} {
		t.Fatalf("report = %v", r)
	}
	media := r["media"].(map[string]any)
	if media["referenced"] != float64(3) || media["inZip"] != float64(3) || len(media["missing"].([]any)) != 0 || len(media["unused"].([]any)) != 0 {
		t.Errorf("media = %v", media)
	}
	if len(e.media.Files) != 3 || len(e.media.Rows) != 3 {
		t.Fatalf("files %d rows %d, want 3 and 3", len(e.media.Files), len(e.media.Rows))
	}
	questions := e.bank.Bank(e.examID).Questions
	if !storedRef.MatchString(questions[0].Stem) || strings.Contains(questions[0].Stem, "img/") {
		t.Errorf("stored stem = %q, want media:<64-hex>", questions[0].Stem)
	}
	for i := 0; i < 2; i++ {
		if !storedRef.MatchString(questions[1].Options[i].Image) || len(storedRef.FindString(questions[1].Options[i].Image)) != 70 {
			t.Errorf("option %d image = %q, want media:<64-hex>", i, questions[1].Options[i].Image)
		}
	}
}

func TestADryRunOfAZipReportsTheMediaAndStoresNothing(t *testing.T) { // FR-4 dry run, media.unused
	e := newZipEnv(t, zipCap)
	data := buildZip(t,
		zent{name: "questions.json", data: example(t, "moto-images.json")},
		zent{name: "img/entrada-prohibida.png", data: pngOf(t, 10, 10)}, zent{name: "img/ceda-el-paso.png", data: pngOf(t, 11, 10)},
		zent{name: "img/stop.png", data: pngOf(t, 12, 10)}, zent{name: "img/unused.png", data: pngOf(t, 13, 10)}, zent{name: "README.txt", data: []byte("hi")})

	r := importReport(t, e.post(t, importPath(e.examID, "dryRun=true"), "x.zip", data), http.StatusOK)

	media := r["media"].(map[string]any)
	unused := media["unused"].([]any)
	if r["applied"] != false || media["referenced"] != float64(3) || media["inZip"] != float64(5) || len(unused) != 2 || unused[0] != "README.txt" || unused[1] != "img/unused.png" {
		t.Errorf("report = %v", r)
	}
	e.nothingWritten(t)
}

func TestEveryS052FixtureIsRejectedWithItsOwnRuleAndNothingIsWritten(t *testing.T) { // S05-2, AC-12
	jsonWith := func(ref string) zent {
		return zent{name: "questions.json", data: []byte(`{"format":"afwilto-questions","formatVersion":1,"questions":[{"key":"q","type":"single","stem":"![x](media:` + ref + `)","options":[{"text":"a","correct":true},{"text":"b","correct":false}]}]}`)}
	}
	thousandAndOne := append([]zent{{name: "questions.json", data: example(t, "aws-text.json")}}, filler(1000)...)
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)

	zipLevel := map[string]struct {
		data []byte
		rule string
	}{
		"1,001 entries":              {buildZip(t, thousandAndOne...), domain.RuleZipEntries},
		"an entry declaring 200 MiB": {patchDeclaredSize(t, buildZip(t, jsonWith("img/big.png"), zent{name: "img/big.png", data: []byte("x")}), "img/big.png", 200<<20), domain.RuleZipSize},
		"../x.png":                   {buildZip(t, jsonWith("img/a.png"), zent{name: "../x.png", data: pngOf(t, 4, 4)}), domain.RuleZipPath},
		"/abs.png":                   {buildZip(t, jsonWith("img/a.png"), zent{name: "/abs.png", data: pngOf(t, 4, 4)}), domain.RuleZipPath},
		"a symlink entry":            {buildZip(t, jsonWith("img/a.png"), zent{name: "img/a.png", data: []byte("/etc/passwd"), mode: fs.ModeSymlink | 0o777}), domain.RuleZipPath},
		"two root JSON files":        {buildZip(t, jsonWith("img/a.png"), zent{name: "second.json", data: []byte("{}")}, zent{name: "img/a.png", data: pngOf(t, 4, 4)}), domain.RuleZipLayout},
	}
	for name, tc := range zipLevel {
		t.Run(name, func(t *testing.T) {
			e := newZipEnv(t, zipCap)

			wantError(t, e.post(t, importPath(e.examID, ""), "x.zip", tc.data), http.StatusUnprocessableEntity, "validation_failed", "file", tc.rule)

			e.nothingWritten(t)
		})
	}

	imageLevel := map[string]struct {
		image []byte
		rule  string
	}{
		"a 3 MiB PNG":               {noisyPNG(t, 3<<20), domain.RuleMediaSize},
		"a 5000x10 PNG":             {pngOf(t, 5000, 10), domain.RuleMediaDimensions},
		"an SVG named .png (AC-12)": {svg, domain.RuleMediaType},
	}
	for name, tc := range imageLevel {
		t.Run(name, func(t *testing.T) {
			e := newZipEnv(t, zipCap)
			data := buildZip(t, jsonWith("img/pic.png"), zent{name: "img/pic.png", data: tc.image})

			for _, query := range []string{"", "dryRun=true"} {
				r := importReport(t, e.post(t, importPath(e.examID, query), "x.zip", data), http.StatusUnprocessableEntity)
				if got := reportErrors(r); !slices.Equal(got, []string{"questions[0].stem|" + tc.rule}) || r["applied"] != false {
					t.Errorf("dryRun=%q: errors = %v applied %v", query, got, r["applied"])
				}
			}
			e.nothingWritten(t)
		})
	}
}

func TestAZipReferencingAMissingImageIs422MediaMissingAndWritesNothing(t *testing.T) { // AC-13
	e := newZipEnv(t, zipCap)
	data := buildZip(t, zent{name: "questions.json", data: example(t, "moto-images.json")}, zent{name: "img/stop.png", data: pngOf(t, 4, 4)})

	dry := importReport(t, e.post(t, importPath(e.examID, "dryRun=true"), "x.zip", data), http.StatusUnprocessableEntity)
	applied := importReport(t, e.post(t, importPath(e.examID, ""), "x.zip", data), http.StatusUnprocessableEntity)

	for _, r := range []map[string]any{dry, applied} {
		media := r["media"].(map[string]any)
		if len(media["missing"].([]any)) != 2 || media["referenced"] != float64(3) {
			t.Errorf("media = %v, want 2 missing of 3", media)
		}
		for _, e := range reportErrors(r) {
			if !strings.HasSuffix(e, "|media_missing") {
				t.Errorf("error %s", e)
			}
		}
	}
	e.nothingWritten(t)
}

func TestAZipWithSchemaErrorsInItsJSONIs422AndWritesNothing(t *testing.T) { // FR-4 stage order inside a zip
	e := newZipEnv(t, zipCap)
	data := buildZip(t, zent{name: "questions.json", data: []byte(`{"format":"wrong","formatVersion":1,"questions":[]}`)}, zent{name: "img/a.png", data: pngOf(t, 4, 4)})

	r := importReport(t, e.post(t, importPath(e.examID, ""), "x.zip", data), http.StatusUnprocessableEntity)

	if len(reportErrors(r)) == 0 || reportErrors(r)[0][len(reportErrors(r)[0])-6:] != "schema" {
		t.Errorf("errors = %v", reportErrors(r))
	}
	e.nothingWritten(t)
}

func TestAZipAboveFiftyMiBIs413(t *testing.T) { // FR-4 sizes
	e := newZipEnv(t, 80<<20)
	big := buildZip(t, zent{name: "questions.json", data: example(t, "aws-text.json")}, zent{name: "pad.bin", data: bytes.Repeat([]byte{7}, 51<<20), store: true})

	rec := e.post(t, importPath(e.examID, ""), "x.zip", big)

	decodeError(t, rec, http.StatusRequestEntityTooLarge, "payload_too_large")
}

func TestARootJSONAboveTenMiBInsideAZipIsZipSize(t *testing.T) { // the .json limit also holds inside a zip
	e := newZipEnv(t, zipCap)
	padding := bytes.Repeat([]byte(" "), 10<<20+10)
	data := buildZip(t, zent{name: "questions.json", data: append(example(t, "aws-text.json"), padding...)})

	wantError(t, e.post(t, importPath(e.examID, ""), "x.zip", data), http.StatusUnprocessableEntity, "validation_failed", "file", domain.RuleZipSize)
}

// ── serving ─────────────────────────────────────────────────────────────────

func TestMediaIsServedWithTheImmutableCacheHeadersAndTheSameBytesTwice(t *testing.T) { // S05-4, AC-10
	e := newZipEnv(t, zipCap)
	png := pngOf(t, 10, 10)
	e.post(t, importPath(e.examID, ""), "x.zip", buildZip(t,
		zent{name: "questions.json", data: example(t, "moto-images.json")}, zent{name: "img/entrada-prohibida.png", data: png},
		zent{name: "img/ceda-el-paso.png", data: pngOf(t, 11, 10)}, zent{name: "img/stop.png", data: pngOf(t, 12, 10)}))
	sha := sumHex(png)

	first := e.get(t, "/api/v1/media/"+sha)
	second := e.get(t, "/api/v1/media/"+sha)

	for i, rec := range []*httptest.ResponseRecorder{first, second} {
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: status %d", i, rec.Code)
		}
		h := rec.Header()
		if h.Get("Content-Type") != "image/png" || h.Get("Cache-Control") != "public, max-age=31536000, immutable" ||
			h.Get("X-Content-Type-Options") != "nosniff" {
			t.Errorf("request %d headers = %v", i, h)
		}
		if !bytes.Equal(rec.Body.Bytes(), png) {
			t.Errorf("request %d returned other bytes", i)
		}
	}
	if !headersEqual(first.Header(), second.Header()) {
		t.Errorf("headers differ between requests: %v vs %v", first.Header(), second.Header())
	}
}

func headersEqual(a, b http.Header) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if !slices.Equal(v, b[k]) {
			return false
		}
	}
	return true
}

func TestAMalformedOrUnknownMediaHashIs404(t *testing.T) { // S05-4 GET /media/xyz
	e := newZipEnv(t, zipCap)

	for _, sha := range []string{"xyz", strings.Repeat("0", 64), strings.Repeat("A", 64), strings.Repeat("a", 63), "..%2F..%2Fetc%2Fpasswd"} {
		rec := e.get(t, "/api/v1/media/"+sha)
		decodeError(t, rec, http.StatusNotFound, "not_found")
	}
}

func TestMediaIsServedWithTheContentTypeItWasStoredWith(t *testing.T) {
	e := newZipEnv(t, zipCap)
	sha := strings.Repeat("c", 64)
	e.media.Seed(domain.Media{SHA256: sha, ContentType: "image/webp", Bytes: 4, Width: 1, Height: 1}, []byte("webp"))

	rec := e.get(t, "/api/v1/media/"+sha)

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/webp" || rec.Body.String() != "webp" {
		t.Errorf("status %d type %q body %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}

func TestAStoredRowWithoutItsFileIs500NotA404(t *testing.T) { // a server fault must not hide as "not found"
	e := newZipEnv(t, zipCap)
	sha := strings.Repeat("d", 64)
	e.media.Rows[sha] = domain.Media{SHA256: sha, ContentType: "image/png", Bytes: 1, Width: 1, Height: 1}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/media/"+sha, nil)
	rec := httptest.NewRecorder()

	e.router.ServeHTTP(rec, req)

	decodeError(t, rec, http.StatusInternalServerError, "internal")
}

// ── export ──────────────────────────────────────────────────────────────────

func unzip(t *testing.T, data []byte) map[string][]byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("the export is not a zip: %v", err)
	}
	out := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = b
	}
	return out
}

func entryNames(m map[string][]byte) []string {
	var names []string
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func TestExportThenReimportIntoTheSameExamReportsEveryQuestionUnchanged(t *testing.T) { // S05-3, FR-5
	e := newZipEnv(t, zipCap)
	e.post(t, importPath(e.examID, ""), "moto-images.zip", motoImagesZip(t))

	rec := e.get(t, "/api/v1/exams/"+e.examID+"/questions/export")

	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("status %d type %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	files := unzip(t, rec.Body.Bytes())
	names := entryNames(files)
	if len(names) != 4 || !slices.Contains(names, "questions.json") || !slices.Contains(names, "img/"+sumHex(pngOf(t, 10, 10))+".png") {
		t.Fatalf("entries = %v", names)
	}
	for _, n := range names {
		if n != "questions.json" && !regexp.MustCompile(`^img/[0-9a-f]{64}\.png$`).MatchString(n) {
			t.Errorf("unexpected entry %q", n)
		}
	}
	var exported struct {
		Format        string `json:"format"`
		FormatVersion int    `json:"formatVersion"`
		Source        struct{ Kind, Name string }
		Questions     []struct{ Stem string }
	}
	if err := json.Unmarshal(files["questions.json"], &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Format != "afwilto-questions" || exported.FormatVersion != 1 || len(exported.Questions) != 2 || exported.Source.Kind != "own" ||
		!regexp.MustCompile(`media:img/[0-9a-f]{64}\.png`).MatchString(exported.Questions[0].Stem) {
		t.Errorf("questions.json = %s", files["questions.json"])
	}

	again := importReport(t, e.post(t, importPath(e.examID, "dryRun=true"), "export.zip", rec.Body.Bytes()), http.StatusOK)

	media := again["media"].(map[string]any)
	if counts(again) != [4]float64{0, 0, 2, 0} || len(again["newCategories"].([]any)) != 0 || len(media["missing"].([]any)) != 0 || len(again["errors"].([]any)) != 0 {
		t.Errorf("re-import report = %v", again)
	}
}

func TestExportOfAMixedBankKeepsEachQuestionsSourceAndRoundTrips(t *testing.T) { // S05-3 mixed TD + Whizlabs, FR-2 source override
	e := newZipEnv(t, zipCap)
	body := func(key, kind, name string) string {
		return `{"key":"` + key + `","type":"single","stem":"s ` + key + `","options":[{"text":"a","correct":true},{"text":"b","correct":false}],"source":{"kind":"` + kind + `","name":"` + name + `"},"categories":{"Dominio":["A"],"Servicio":["S3","IAM"]},"origin":{"set":"Set 1","number":3},"difficulty":"hard","explanation":"because"}`
	}
	file := `{"format":"afwilto-questions","formatVersion":1,"questions":[` +
		strings.Join([]string{body("wl-1", "purchased", "Whizlabs"), body("td-1", "purchased", "Tutorials Dojo"), body("td-2", "purchased", "Tutorials Dojo"), body("own-1", "own", "Mine")}, ",") + `]}`
	if rec := e.post(t, importPath(e.examID, ""), "q.json", []byte(file)); rec.Code != http.StatusOK {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}

	rec := e.get(t, "/api/v1/exams/"+e.examID+"/questions/export")

	var exported struct {
		Source    *struct{ Kind, Name string }
		Questions []struct {
			Key    string
			Source *struct{ Kind, Name string }
		}
	}
	if err := json.Unmarshal(unzip(t, rec.Body.Bytes())["questions.json"], &exported); err != nil {
		t.Fatal(err)
	}
	if exported.Source == nil || exported.Source.Name != "Tutorials Dojo" {
		t.Fatalf("file source = %+v, want the most common one", exported.Source)
	}
	overrides := map[string]string{}
	for _, q := range exported.Questions {
		if q.Source != nil {
			overrides[q.Key] = q.Source.Name
		}
	}
	if len(overrides) != 2 || overrides["wl-1"] != "Whizlabs" || overrides["own-1"] != "Mine" {
		t.Errorf("overrides = %v, want only the questions whose source differs", overrides)
	}

	again := importReport(t, e.post(t, importPath(e.examID, "dryRun=true"), "export.zip", rec.Body.Bytes()), http.StatusOK)
	if counts(again) != [4]float64{0, 0, 4, 0} || len(again["newCategories"].([]any)) != 0 || len(again["newClassifications"].([]any)) != 0 {
		t.Errorf("re-import report = %v", again)
	}
}

func TestExportOfAnExamWithoutQuestionsIsAZipWithAnEmptyQuestionList(t *testing.T) { // documented choice: 200, not 404
	e := newZipEnv(t, zipCap)

	rec := e.get(t, "/api/v1/exams/"+e.examID+"/questions/export")

	files := unzip(t, rec.Body.Bytes())
	if rec.Code != http.StatusOK || !slices.Equal(entryNames(files), []string{"questions.json"}) || !strings.Contains(string(files["questions.json"]), `"questions": []`) {
		t.Errorf("status %d entries %v body %s", rec.Code, entryNames(files), files["questions.json"])
	}
}

func TestExportOfAnUnknownExamIs404(t *testing.T) {
	e := newZipEnv(t, zipCap)

	decodeError(t, e.get(t, "/api/v1/exams/"+unknownID+"/questions/export"), http.StatusNotFound, "not_found")
}

func TestExportNamesTheDownloadAndDoesNotLetTheBrowserSniffIt(t *testing.T) {
	e := newZipEnv(t, zipCap)

	rec := e.get(t, "/api/v1/exams/"+e.examID+"/questions/export")

	if got := rec.Header().Get("Content-Disposition"); got != `attachment; filename="questions.zip"` {
		t.Errorf("Content-Disposition = %q", got)
	}
}

func TestExportAndMediaRoutesExistOnlyWhenTheirUseCasesAreWired(t *testing.T) { // Register keeps earlier shapes
	const id = "0192f3a4-0000-7000-8000-000000000001"
	sha := strings.Repeat("a", 64)
	routes := []struct{ method, path, want string }{
		{http.MethodGet, "/api/v1/media/" + sha, "GET /api/v1/media/{sha256}"},
		{http.MethodGet, "/api/v1/exams/" + id + "/questions/export", "GET /api/v1/exams/{id}/questions/export"},
	}
	none, full := http.NewServeMux(), http.NewServeMux()
	Register(Deps{})(none)
	e := newZipEnv(t, zipCap)
	_ = e
	deps := newBankDeps(t)
	media := testsupport.NewFakeMedia()
	deps.GetMedia = usecase.NewGetMediaService(media, media)
	deps.ExportQuestions = usecase.NewExportQuestionsService(testsupport.NewFakeQuestionBank(testsupport.NewFakeExamRepo(testsupport.NewFakeSubjectDirectory())), media)
	Register(deps)(full)
	onlyExport := http.NewServeMux()
	deps.GetMedia = nil
	Register(deps)(onlyExport)

	for _, tc := range routes {
		if _, p := none.Handler(httptest.NewRequest(tc.method, tc.path, nil)); p != "" {
			t.Errorf("unwired %s: %q", tc.path, p)
		}
		if _, p := full.Handler(httptest.NewRequest(tc.method, tc.path, nil)); p != tc.want {
			t.Errorf("wired %s: %q, want %q", tc.path, p, tc.want)
		}
	}
	if _, p := onlyExport.Handler(httptest.NewRequest(http.MethodGet, routes[1].path, nil)); p != "" {
		t.Errorf("export without GetMedia is registered as %q: it could not stream the images", p)
	}
}
