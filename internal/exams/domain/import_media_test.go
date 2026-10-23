package domain

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// png builds an inspected, usable image entry of a zip.
func png(path, sha string) MediaFile {
	return MediaFile{Path: path, Data: []byte("bytes of " + path),
		Media: Media{SHA256: sha, ContentType: "image/png", Bytes: 9, Width: 10, Height: 10}}
}

func archiveOf(names []string, files ...MediaFile) *MediaArchive {
	a := &MediaArchive{Names: names, Files: map[string]MediaFile{}}
	for _, f := range files {
		a.Files[f.Path] = f
	}
	return a
}

func planWithMedia(t *testing.T, bank Bank, f QuestionFile, archive *MediaArchive, known ...string) ImportPlan {
	t.Helper()
	g := &ids{}
	if bank.Questions != nil || bank.Classifications != nil {
		g.n = 500
	}
	set := map[string]bool{}
	for _, k := range known {
		set[k] = true
	}
	return PlanImport(PlanInput{ExamID: "exam-1", Bank: bank, File: f, NewID: g.next, Now: planNow, Archive: archive, KnownMedia: set})
}

func withStem(q FileQuestion, stem string) FileQuestion { q.Stem = stem; return q }

func TestPlanImportRewritesZipReferencesToTheirHashAndReportsTheMedia(t *testing.T) { // AC-10, S05-1, FR-4 media report
	q := withStem(fq("m"), "Look ![Señal](media:img/r101.png)")
	q.Options[0].Image, q.Options[0].Alt = "media:img/opt.png", "alt"
	archive := archiveOf([]string{"img/opt.png", "img/r101.png", "img/spare.png"}, png("img/r101.png", shaA), png("img/opt.png", shaB))

	plan := planWithMedia(t, Bank{}, file(q), archive)

	if len(plan.Report.Errors) != 0 {
		t.Fatalf("errors = %v", plan.Report.Errors)
	}
	write := mustWrite(t, plan)
	stored := write.Creates[0].Question
	if stored.Stem != "Look ![Señal](media:"+shaA+")" || stored.Options[0].Image != "media:"+shaB {
		t.Errorf("stored stem %q, option image %q", stored.Stem, stored.Options[0].Image)
	}
	if got := plan.Report.Preview[0].Stem; got != stored.Stem {
		t.Errorf("preview stem %q, want the stored form %q", got, stored.Stem)
	}
	m := plan.Report.Media
	if m.Referenced != 2 || m.InZip != 3 || len(m.Missing) != 0 || !slices.Equal(m.Unused, []string{"img/spare.png"}) {
		t.Errorf("media report = %+v, want 2 referenced, 3 in zip, none missing, img/spare.png unused", m)
	}
	var shas []string
	for _, f := range write.Media {
		shas = append(shas, f.Media.SHA256)
	}
	if !slices.Equal(shas, []string{shaA, shaB}) {
		t.Errorf("write.Media hashes = %v, want only the referenced images, in order of first use", shas)
	}
}

func TestPlanImportStoresAnImageUsedByFortyQuestionsOnce(t *testing.T) { // AC-11
	var qs []FileQuestion
	for i := 0; i < 40; i++ {
		qs = append(qs, withStem(fq(fmt.Sprintf("q%02d", i)), "![x](media:img/shared.png)"))
	}
	archive := archiveOf([]string{"img/shared.png"}, png("img/shared.png", shaA))

	plan := planWithMedia(t, Bank{}, file(qs...), archive)

	if got := len(mustWrite(t, plan).Media); got != 1 {
		t.Errorf("write.Media = %d images, want 1", got)
	}
	if plan.Report.Media.Referenced != 1 {
		t.Errorf("referenced = %d, want 1 distinct reference", plan.Report.Media.Referenced)
	}
}

func TestPlanImportTwoPathsWithTheSameBytesAreOneImage(t *testing.T) { // content addressing
	q := withStem(fq("m"), "![a](media:img/a.png) ![b](media:img/copy-of-a.png)")
	archive := archiveOf([]string{"img/a.png", "img/copy-of-a.png"}, png("img/a.png", shaA), png("img/copy-of-a.png", shaA))

	plan := planWithMedia(t, Bank{}, file(q), archive)

	write := mustWrite(t, plan)
	if got := write.Creates[0].Question.Stem; got != "![a](media:"+shaA+") ![b](media:"+shaA+")" {
		t.Errorf("stem = %q", got)
	}
	if len(write.Media) != 1 {
		t.Errorf("write.Media = %d, want 1", len(write.Media))
	}
}

func TestPlanImportAReferenceWithoutAnEntryIsMediaMissingAtItsLocation(t *testing.T) { // AC-13, BR-Q4
	q := withStem(fq("m"), "![a](media:img/there.png) ![b](media:img/missing.png)")
	q.Explanation = "![c](media:img/missing.png)"
	archive := archiveOf([]string{"img/there.png"}, png("img/there.png", shaA))

	plan := planWithMedia(t, Bank{}, file(q), archive)

	if got, want := pathsOf(plan.Report.Errors), []string{"questions[0].stem|media_missing", "questions[0].explanation|media_missing"}; !slices.Equal(got, want) {
		t.Errorf("errors = %v, want %v", got, want)
	}
	m := plan.Report.Media
	if m.Referenced != 2 || m.InZip != 1 || !slices.Equal(m.Missing, []string{"img/missing.png"}) || len(m.Unused) != 0 {
		t.Errorf("media = %+v", m)
	}
	if plan.Write != nil {
		t.Error("a plan with errors carries no write")
	}
}

func TestPlanImportAHashReferenceNeedsAStoredImage(t *testing.T) { // BR-Q4, slice 05 §1.3 second bullet
	known := withStem(fq("known"), "![a](media:"+shaA+")")
	unknown := withStem(fq("unknown"), "![b](media:"+shaB+")")

	good := planWithMedia(t, Bank{}, file(known), nil, shaA)
	bad := planWithMedia(t, Bank{}, file(known, unknown), nil, shaA)

	goodWrite := mustWrite(t, good)
	if len(good.Report.Errors) != 0 || len(goodWrite.Media) != 0 {
		t.Errorf("a stored hash must resolve with nothing to store: errors %v write %+v", good.Report.Errors, goodWrite)
	}
	if goodWrite.Creates[0].Question.Stem != "![a](media:"+shaA+")" {
		t.Errorf("stem = %q", goodWrite.Creates[0].Question.Stem)
	}
	if got := pathsOf(bad.Report.Errors); !slices.Equal(got, []string{"questions[1].stem|media_missing"}) {
		t.Errorf("errors = %v", got)
	}
	if !slices.Equal(bad.Report.Media.Missing, []string{shaB}) || bad.Report.Media.Referenced != 2 {
		t.Errorf("media = %+v", bad.Report.Media)
	}
}

func TestPlanImportAnImageProblemIsReportedOnceAtTheFirstUseWithItsOwnRule(t *testing.T) { // S05-2 media rules, AC-12
	for _, rule := range []string{RuleMediaType, RuleMediaSize, RuleMediaDimensions} {
		t.Run(rule, func(t *testing.T) {
			bad := MediaFile{Path: "img/bad.png", Problem: &MediaProblem{Rule: rule, Message: "img/bad.png: " + rule + " problem"}}
			a := withStem(fq("a"), "![x](media:img/bad.png)")
			b := withStem(fq("b"), "![y](media:img/bad.png)")
			archive := archiveOf([]string{"img/bad.png"}, bad)

			plan := planWithMedia(t, Bank{}, file(a, b), archive)

			if got, want := pathsOf(plan.Report.Errors), []string{"questions[0].stem|" + rule}; !slices.Equal(got, want) {
				t.Errorf("errors = %v, want %v", got, want)
			}
			if !strings.Contains(plan.Report.Errors[0].Message, rule+" problem") || plan.Write != nil {
				t.Errorf("message %q, write %v", plan.Report.Errors[0].Message, plan.Write)
			}
		})
	}
}

func TestPlanImportAnUnreferencedBadImageIsJustUnused(t *testing.T) { // slice 05 §1.2 last bullet
	bad := MediaFile{Path: "img/bad.png", Problem: &MediaProblem{Rule: RuleMediaType, Message: "not an image"}}
	archive := archiveOf([]string{"img/bad.png", "notes.txt"}, bad)

	plan := planWithMedia(t, Bank{}, file(fq("m")), archive)

	if len(plan.Report.Errors) != 0 || !slices.Equal(plan.Report.Media.Unused, []string{"img/bad.png", "notes.txt"}) {
		t.Errorf("errors %v, unused %v", plan.Report.Errors, plan.Report.Media.Unused)
	}
	if got := len(mustWrite(t, plan).Media); got != 0 {
		t.Errorf("an unreferenced entry must not be stored: %d", got)
	}
}

func TestPlanImportUnusedEntriesAreListedSorted(t *testing.T) {
	archive := archiveOf([]string{"z.png", "a.png", "img/m.png"})

	plan := planWithMedia(t, Bank{}, file(fq("m")), archive)

	if !slices.Equal(plan.Report.Media.Unused, []string{"a.png", "img/m.png", "z.png"}) {
		t.Errorf("unused = %v", plan.Report.Media.Unused)
	}
}

func TestPlanImportReimportOfAnExportReportsEveryQuestionUnchanged(t *testing.T) { // S05-3 domain half, BR-Q4
	stored := withStem(fq("m", C("Dominio", "A")), "Look ![x]("+MediaScheme+shaA+")")
	first := planWithMedia(t, Bank{}, file(stored), nil, shaA)
	bank := Bank{}.After(*mustWrite(t, first))

	exported := withStem(fq("m", C("Dominio", "A")), "Look ![x](media:img/"+shaA+".png)")
	archive := archiveOf([]string{"img/" + shaA + ".png"}, png("img/"+shaA+".png", shaA))
	again := planWithMedia(t, bank, file(exported), archive)

	if again.Report.Counts != (ImportCounts{Unchanged: 1}) {
		t.Errorf("counts = %+v, want 1 unchanged; errors %v", again.Report.Counts, again.Report.Errors)
	}
	if len(again.Report.NewCategories) != 0 || len(again.Report.Media.Missing) != 0 {
		t.Errorf("new categories %v, missing %v", again.Report.NewCategories, again.Report.Media.Missing)
	}
}

func TestPlanImportMediaErrorsFollowTheTwoHundredErrorCap(t *testing.T) { // the 200-error cap
	var qs []FileQuestion
	for i := 0; i < 250; i++ {
		qs = append(qs, withStem(fq(fmt.Sprintf("q%03d", i)), "![x](media:img/missing.png)"))
	}

	plan := planWithMedia(t, Bank{}, file(qs...), archiveOf(nil))

	if len(plan.Report.Errors) != MaxReportErrors || len(plan.Report.Warnings) != 1 || !strings.Contains(plan.Report.Warnings[0], "50 more errors") {
		t.Errorf("errors %d, warnings %v", len(plan.Report.Errors), plan.Report.Warnings)
	}
}

func TestPlanImportSemanticErrorsStillWinOverMediaErrors(t *testing.T) { // FR-4 order
	dup := withStem(fq("dup"), "![x](media:img/missing.png)")

	plan := planWithMedia(t, Bank{}, file(fq("dup"), dup), archiveOf(nil))

	if got := pathsOf(plan.Report.Errors); !slices.Equal(got, []string{"questions[1].key|duplicate_key"}) {
		t.Errorf("errors = %v", got)
	}
}
