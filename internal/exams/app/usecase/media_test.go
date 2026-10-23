package usecase

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"
	"strings"
	"testing"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
)

const (
	shaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

type mediaEnv struct {
	importEnv
	media *testsupport.FakeMedia
}

func newMediaEnv(t *testing.T) mediaEnv {
	t.Helper()
	e := newEnv()
	media := testsupport.NewFakeMedia()
	e.bank.AttachMedia(media)
	exam := e.add(t, subjectA, "SAA-C03")
	imp := NewImportQuestionsService(e.bank, &testsupport.StepClock{}, &testsupport.SeqIDGen{N: 1000}, WithMedia(media, media))
	return mediaEnv{importEnv: importEnv{env: e, imp: imp, examID: exam.ID}, media: media}
}

func image(path, sha, content string) domain.MediaFile {
	return domain.MediaFile{Path: path, Data: []byte(content),
		Media: domain.Media{SHA256: sha, ContentType: "image/png", Bytes: len(content), Width: 10, Height: 10}}
}

func archive(names []string, files ...domain.MediaFile) *domain.MediaArchive {
	a := &domain.MediaArchive{Names: names, Files: map[string]domain.MediaFile{}}
	for _, f := range files {
		a.Files[f.Path] = f
	}
	return a
}

func withStem(q domain.FileQuestion, stem string) domain.FileQuestion { q.Stem = stem; return q }

func (e mediaEnv) importArchive(t *testing.T, dryRun bool, file *domain.QuestionFile, a *domain.MediaArchive) domain.ImportReport {
	t.Helper()
	r, err := e.imp.Execute(ctx, in.ImportQuestionsCommand{ExamID: e.examID, DryRun: dryRun, File: file, Archive: a})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return r
}

func TestApplyStoresTheReferencedImagesAndTheQuestionsHoldTheirHashes(t *testing.T) { // AC-10, S05-1
	e := newMediaEnv(t)
	q := withStem(fq("m"), "![Señal](media:img/r101.png)")
	a := archive([]string{"img/r101.png", "img/spare.png"}, image("img/r101.png", shaA, "png-a"))

	r := e.importArchive(t, false, fileOf(q), a)

	if !r.Applied || len(r.Errors) != 0 {
		t.Fatalf("applied %v errors %v", r.Applied, r.Errors)
	}
	if got := string(e.media.Files[shaA]); got != "png-a" || len(e.media.Files) != 1 {
		t.Errorf("stored files = %v, want only the referenced image", e.media.Files)
	}
	if e.media.Rows[shaA].ContentType != "image/png" {
		t.Errorf("rows = %v", e.media.Rows)
	}
	if got := e.bank.Bank(e.examID).Questions[0].Stem; got != "![Señal](media:"+shaA+")" {
		t.Errorf("stored stem = %q", got)
	}
	if !slices.Equal(r.Media.Unused, []string{"img/spare.png"}) {
		t.Errorf("unused = %v", r.Media.Unused)
	}
}

func TestAnImageUsedByFortyQuestionsIsStoredOnce(t *testing.T) { // AC-11
	e := newMediaEnv(t)
	var qs []domain.FileQuestion
	for i := 0; i < 40; i++ {
		qs = append(qs, withStem(fq(fmt.Sprintf("q%02d", i)), "![x](media:img/shared.png)"))
	}

	e.importArchive(t, false, fileOf(qs...), archive([]string{"img/shared.png"}, image("img/shared.png", shaA, "png-a")))

	if e.media.Puts != 1 || len(e.media.Rows) != 1 {
		t.Errorf("puts %d, rows %d; want 1 and 1", e.media.Puts, len(e.media.Rows))
	}
}

func TestADryRunStoresNoFileAndNoRow(t *testing.T) { // FR-4 dry run writes nothing
	e := newMediaEnv(t)
	q := withStem(fq("m"), "![x](media:img/a.png)")

	r := e.importArchive(t, true, fileOf(q), archive([]string{"img/a.png"}, image("img/a.png", shaA, "png-a")))

	if r.Applied || len(r.Errors) != 0 || r.Media.Referenced != 1 {
		t.Errorf("report = %+v", r)
	}
	if e.media.Puts != 0 || len(e.media.Files) != 0 || len(e.media.Rows) != 0 || e.bank.Applied != 0 {
		t.Errorf("puts %d files %d rows %d applied %d; want nothing written", e.media.Puts, len(e.media.Files), len(e.media.Rows), e.bank.Applied)
	}
}

func TestAnImportWithErrorsStoresNoFile(t *testing.T) { // S05-2 "nothing is written", AC-13
	for name, tc := range map[string]struct {
		file *domain.QuestionFile
		a    *domain.MediaArchive
		rule string
	}{
		"a missing reference": {fileOf(withStem(fq("m"), "![x](media:img/a.png) ![y](media:img/gone.png)")), archive([]string{"img/a.png"}, image("img/a.png", shaA, "a")), domain.RuleMediaMissing},
		"a bad image": {fileOf(withStem(fq("m"), "![x](media:img/a.png) ![y](media:img/bad.png)")),
			archive([]string{"img/a.png", "img/bad.png"}, image("img/a.png", shaA, "a"),
				domain.MediaFile{Path: "img/bad.png", Problem: &domain.MediaProblem{Rule: domain.RuleMediaType, Message: "not an image"}}), domain.RuleMediaType},
		"a semantic error": {fileOf(withStem(fq("m"), "![x](media:img/a.png)"), fq("m")), archive([]string{"img/a.png"}, image("img/a.png", shaA, "a")), domain.RuleDuplicateKey},
	} {
		t.Run(name, func(t *testing.T) {
			e := newMediaEnv(t)

			r := e.importArchive(t, false, tc.file, tc.a)

			if r.Applied || len(r.Errors) == 0 || r.Errors[0].Rule != tc.rule {
				t.Fatalf("report applied %v errors %v, want %s", r.Applied, r.Errors, tc.rule)
			}
			if e.media.Puts != 0 || len(e.media.Files) != 0 || len(e.media.Rows) != 0 || e.bank.Applied != 0 {
				t.Errorf("puts %d files %d rows %d applied %d; want nothing written", e.media.Puts, len(e.media.Files), len(e.media.Rows), e.bank.Applied)
			}
		})
	}
}

func TestTheFileIsWrittenBeforeTheImportCommits(t *testing.T) { // slice 05 §1.4 file before the transaction commits
	e := newMediaEnv(t)
	appliedAtPut := -1
	e.media.OnPut = func(string) { appliedAtPut = e.bank.Applied }

	e.importArchive(t, false, fileOf(withStem(fq("m"), "![x](media:img/a.png)")), archive([]string{"img/a.png"}, image("img/a.png", shaA, "a")))

	if appliedAtPut != 0 || e.bank.Applied != 1 {
		t.Errorf("imports applied when the file was put = %d (after: %d); want 0 then 1", appliedAtPut, e.bank.Applied)
	}
}

func TestAFailedFileWriteFailsTheImportAndCommitsNothing(t *testing.T) {
	e := newMediaEnv(t)
	e.media.PutErr = errors.New("disk full")

	_, err := e.imp.Execute(ctx, in.ImportQuestionsCommand{ExamID: e.examID, File: fileOf(withStem(fq("m"), "![x](media:img/a.png)")),
		Archive: archive([]string{"img/a.png"}, image("img/a.png", shaA, "a"))})

	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Errorf("err = %v, want the storage failure", err)
	}
	if e.bank.Applied != 0 || len(e.media.Rows) != 0 {
		t.Errorf("applied %d rows %d; want nothing committed", e.bank.Applied, len(e.media.Rows))
	}
}

func TestAJSONImportMayReferenceAStoredHashButNotAnUnknownOne(t *testing.T) { // BR-Q4, slice 05 §1.3
	e := newMediaEnv(t)
	e.media.Seed(domain.Media{SHA256: shaA, ContentType: "image/png", Bytes: 1, Width: 1, Height: 1}, []byte("a"))
	known := withStem(fq("known"), "![x](media:"+shaA+")")
	unknown := withStem(fq("unknown"), "![y](media:"+shaB+")")

	ok := e.importArchive(t, false, fileOf(known), nil)
	bad := e.importArchive(t, false, fileOf(fq("other"), unknown), nil)

	if !ok.Applied || len(ok.Errors) != 0 || e.media.Puts != 0 {
		t.Errorf("stored hash: applied %v errors %v puts %d", ok.Applied, ok.Errors, e.media.Puts)
	}
	if bad.Applied || len(bad.Errors) != 1 || bad.Errors[0].Rule != domain.RuleMediaMissing || bad.Errors[0].Path != "questions[1].stem" {
		t.Errorf("unknown hash: applied %v errors %v", bad.Applied, bad.Errors)
	}
}

func TestAFailedMediaLookupIsAnErrorNotAMissingImage(t *testing.T) { // access failure vs valid empty result
	e := newMediaEnv(t)
	e.media.FindErr = errors.New("database down")

	_, err := e.imp.Execute(ctx, in.ImportQuestionsCommand{ExamID: e.examID, File: fileOf(withStem(fq("m"), "![x](media:"+shaA+")"))})

	if err == nil || !strings.Contains(err.Error(), "database down") {
		t.Errorf("err = %v, want the lookup failure to surface", err)
	}
}

func TestReimportingAnExportReportsEveryQuestionUnchanged(t *testing.T) { // S05-3 use-case half
	e := newMediaEnv(t)
	first := fileOf(withStem(fq("m", cn("Dominio", "A")), "![x](media:img/a.png)"))
	e.importArchive(t, false, first, archive([]string{"img/a.png"}, image("img/a.png", shaA, "a")))

	// an export names the same bytes img/<sha>.png
	path := "img/" + shaA + ".png"
	again := fileOf(withStem(fq("m", cn("Dominio", "A")), "![x](media:"+path+")"))
	r := e.importArchive(t, true, again, archive([]string{path}, image(path, shaA, "a")))

	if r.Counts.Unchanged != 1 || r.Counts.Create+r.Counts.Update != 0 || len(r.NewCategories) != 0 || len(r.Media.Missing) != 0 {
		t.Errorf("counts %+v new categories %v missing %v", r.Counts, r.NewCategories, r.Media.Missing)
	}
}

// ── GetMedia ────────────────────────────────────────────────────────────────

func TestGetMediaOpensAStoredImageWithItsRow(t *testing.T) { // S05-4
	media := testsupport.NewFakeMedia()
	row := domain.Media{SHA256: shaA, ContentType: "image/webp", Bytes: 5, Width: 2, Height: 3}
	media.Seed(row, []byte("bytes"))

	got, err := NewGetMediaService(media, media).Execute(ctx, shaA)

	if err != nil {
		t.Fatal(err)
	}
	defer got.Body.Close()
	body, _ := io.ReadAll(got.Body)
	if got.Media != row || string(body) != "bytes" {
		t.Errorf("media %+v body %q", got.Media, body)
	}
}

func TestGetMediaIsNotFoundForAMalformedOrUnknownHashWithoutAskingTheIndex(t *testing.T) { // S05-4 GET /media/xyz
	media := testsupport.NewFakeMedia()
	media.FindErr = errors.New("must not be asked for a malformed hash")
	svc := NewGetMediaService(media, media)

	for _, sha := range []string{"xyz", "", strings.ToUpper(shaA), shaA[:63], "../" + shaA} {
		if _, err := svc.Execute(ctx, sha); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("Execute(%q) = %v, want ErrNotFound", sha, err)
		}
	}
	media.FindErr = nil
	if _, err := svc.Execute(ctx, shaB); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown hash = %v, want ErrNotFound", err)
	}
}

func TestGetMediaWithARowButNoFileIsAServerFaultNotANotFound(t *testing.T) {
	media := testsupport.NewFakeMedia()
	media.Rows[shaA] = domain.Media{SHA256: shaA, ContentType: "image/png", Bytes: 1, Width: 1, Height: 1}

	_, err := NewGetMediaService(media, media).Execute(ctx, shaA)

	if err == nil || errors.Is(err, domain.ErrNotFound) || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want the storage error and not ErrNotFound", err)
	}
}

func TestGetMediaSurfacesAnIndexFailure(t *testing.T) {
	media := testsupport.NewFakeMedia()
	media.FindErr = errors.New("database down")

	_, err := NewGetMediaService(media, media).Execute(ctx, shaA)

	if err == nil || errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

// ── ExportQuestions ─────────────────────────────────────────────────────────

func TestExportReturnsTheAliveQuestionsAndTheImagesTheyUse(t *testing.T) { // FR-5
	e := newMediaEnv(t)
	e.importArchive(t, false, fileOf(
		withStem(fq("one", cn("Dominio", "A")), "![x](media:img/a.png)"), withStem(fq("two"), "![y](media:img/b.png) ![z](media:img/a.png)"), fq("three")),
		archive([]string{"img/a.png", "img/b.png", "img/c.png"}, image("img/a.png", shaA, "a"), image("img/b.png", shaB, "b")))

	got, err := NewExportQuestionsService(e.bank, e.media).Execute(ctx, e.examID)

	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	for _, q := range got.Questions {
		keys = append(keys, q.Key)
	}
	if !slices.Equal(keys, []string{"one", "two", "three"}) {
		t.Errorf("keys = %v", keys)
	}
	if len(got.Media) != 2 || got.Media[shaA].ContentType != "image/png" || got.Media[shaB].Bytes != 1 {
		t.Errorf("media = %v, want exactly the two used images", got.Media)
	}
}

func TestExportOfAnExamWithoutImagesHasAnEmptyMediaSet(t *testing.T) {
	e := newMediaEnv(t)
	e.importArchive(t, false, fileOf(fq("one")), nil)

	got, err := NewExportQuestionsService(e.bank, e.media).Execute(ctx, e.examID)

	if err != nil || len(got.Questions) != 1 || len(got.Media) != 0 {
		t.Errorf("export = %+v, %v", got, err)
	}
}

func TestExportOfAnUnknownExamIsNotFound(t *testing.T) {
	e := newMediaEnv(t)

	_, err := NewExportQuestionsService(e.bank, e.media).Execute(ctx, "0abcdef0-0000-7000-8000-00000000ffff")

	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v", err)
	}
}

func TestExportFailsWhenAQuestionUsesAnImageTheTableDoesNotKnow(t *testing.T) { // data corruption must not silently drop an image
	e := newMediaEnv(t)
	e.bank.Set(e.examID, domain.Bank{Questions: []domain.Question{{Key: "q", Stem: "![x](media:" + shaA + ")"}}})

	_, err := NewExportQuestionsService(e.bank, e.media).Execute(ctx, e.examID)

	if err == nil || errors.Is(err, domain.ErrNotFound) || !strings.Contains(err.Error(), shaA) {
		t.Errorf("err = %v, want an error naming the hash", err)
	}
}
