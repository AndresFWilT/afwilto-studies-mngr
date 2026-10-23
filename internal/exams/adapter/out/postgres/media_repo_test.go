package postgres

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

const (
	shaOne = "1111111111111111111111111111111111111111111111111111111111111111"
	shaTwo = "2222222222222222222222222222222222222222222222222222222222222222"
)

func png(sha string) domain.MediaFile {
	return domain.MediaFile{Path: "img/" + sha[:4] + ".png", Data: []byte("x"),
		Media: domain.Media{SHA256: sha, ContentType: "image/png", Bytes: 1234, Width: 640, Height: 480}}
}

func newMediaEnv(t *testing.T) (*bankEnv, *MediaRepository) {
	t.Helper()
	b := newBankEnv(t)
	exec(t, `TRUNCATE media`)
	return b, NewMediaRepository(pool)
}

// applyMedia runs ApplyImport with a write that only carries media.
func (b *bankEnv) applyMedia(t *testing.T, media ...domain.MediaFile) {
	t.Helper()
	err := b.bank.ApplyImport(ctx, b.exam.ID, func(domain.Bank) (*domain.ImportWrite, error) {
		return &domain.ImportWrite{ExamID: b.exam.ID, Now: time.Now().UTC(), Media: media}, nil
	})
	if err != nil {
		t.Fatalf("ApplyImport: %v", err)
	}
}

func mediaRows(t *testing.T) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM media`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestMigration0012CreatesTheMediaTableWithTheSpecColumns(t *testing.T) { // slice 05 §3
	skipShort(t)

	got := columnsOf(t, "media")

	want := []string{"bytes", "content_type", "created_at", "height", "sha256", "width"}
	if !slices.Equal(got, want) {
		t.Errorf("columns = %v, want %v", got, want)
	}
}

func TestMigration0012RefusesHardDeletesAndBadRows(t *testing.T) { // slice 05 §3 no-delete trigger, Spec 16 FR-0
	skipShort(t)
	exec(t, `TRUNCATE media`)
	exec(t, `INSERT INTO media (sha256, content_type, bytes, width, height) VALUES ($1, 'image/png', 10, 5, 5)`, shaOne)

	if _, err := pool.Exec(ctx, `DELETE FROM media`); err == nil {
		t.Error("hard DELETE on media succeeded")
	}
	bad := map[string]string{
		"duplicate hash":          `INSERT INTO media (sha256, content_type, bytes, width, height) VALUES ('` + shaOne + `', 'image/png', 10, 5, 5)`,
		"uppercase hash":          `INSERT INTO media (sha256, content_type, bytes, width, height) VALUES ('` + "ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789ABCDEF0123456789" + `', 'image/png', 10, 5, 5)`,
		"short hash":              `INSERT INTO media (sha256, content_type, bytes, width, height) VALUES ('abc', 'image/png', 10, 5, 5)`,
		"svg content type":        `INSERT INTO media (sha256, content_type, bytes, width, height) VALUES ('` + shaTwo + `', 'image/svg+xml', 10, 5, 5)`,
		"zero bytes":              `INSERT INTO media (sha256, content_type, bytes, width, height) VALUES ('` + shaTwo + `', 'image/png', 0, 5, 5)`,
		"zero width":              `INSERT INTO media (sha256, content_type, bytes, width, height) VALUES ('` + shaTwo + `', 'image/png', 10, 0, 5)`,
		"zero height":             `INSERT INTO media (sha256, content_type, bytes, width, height) VALUES ('` + shaTwo + `', 'image/png', 10, 5, 0)`,
		"hash with a non-hex 'g'": `INSERT INTO media (sha256, content_type, bytes, width, height) VALUES ('` + "g111111111111111111111111111111111111111111111111111111111111111" + `', 'image/png', 10, 5, 5)`,
	}
	for name, stmt := range bad {
		if _, err := pool.Exec(ctx, stmt); err == nil {
			t.Errorf("%s: the insert was accepted", name)
		}
	}
}

func TestMigration0012DownDropsTheTableAndUpRestoresIt(t *testing.T) { // slice 05 §3
	skipShort(t)
	m, err := newMigrate()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		defer m.Close()
		if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
			t.Errorf("restoring migrations: %v", err)
		}
	})
	if !tableExists(t, "media") {
		t.Fatal("media missing after migrate up")
	}

	if err := m.Migrate(11); err != nil {
		t.Fatalf("migrate down to version 11: %v", err)
	}
	if tableExists(t, "media") {
		t.Error("media still present after rolling back to version 11")
	}
	if !tableExists(t, "questions") {
		t.Error("questions must survive the rollback of 0012")
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if !tableExists(t, "media") {
		t.Error("media missing after re-applying")
	}
}

func TestApplyImportInsertsTheMediaRowsOfTheWrite(t *testing.T) { // S05-1 storage half
	b, repo := newMediaEnv(t)

	b.applyMedia(t, png(shaOne), png(shaTwo))

	got, err := repo.Find(ctx, []string{shaOne, shaTwo})
	if err != nil {
		t.Fatal(err)
	}
	m := got[shaOne]
	if len(got) != 2 || m.SHA256 != shaOne || m.ContentType != "image/png" || m.Bytes != 1234 || m.Width != 640 || m.Height != 480 || m.CreatedAt.IsZero() {
		t.Errorf("Find = %+v", got)
	}
}

func TestApplyImportOfAnImageThatIsAlreadyStoredKeepsOneRow(t *testing.T) { // AC-11 one Media row
	b, _ := newMediaEnv(t)

	b.applyMedia(t, png(shaOne))
	b.applyMedia(t, png(shaOne), png(shaTwo))

	if n := mediaRows(t); n != 2 {
		t.Errorf("media rows = %d, want 2", n)
	}
}

func TestAnImportThatFailsStoresNoMediaRow(t *testing.T) { // BR-I1: the rows commit with the import
	b, _ := newMediaEnv(t)
	broken := domain.QuestionWrite{Question: domain.Question{
		ID: uuid.NewString(), ExamID: b.exam.ID, Key: "BAD KEY", Type: domain.QuestionSingle, Stem: "s",
		Source: domain.Source{Kind: "own", Name: "x"}, Fingerprint: "f", Version: 1,
		Options: []domain.Option{{Text: "a", Correct: true}, {Text: "b"}},
	}}

	err := b.bank.ApplyImport(ctx, b.exam.ID, func(domain.Bank) (*domain.ImportWrite, error) {
		return &domain.ImportWrite{ExamID: b.exam.ID, Now: time.Now().UTC(), Media: []domain.MediaFile{png(shaOne)}, Creates: []domain.QuestionWrite{broken}}, nil
	})

	if err == nil {
		t.Fatal("the broken write was accepted")
	}
	if n := mediaRows(t); n != 0 {
		t.Errorf("media rows after a failed import = %d, want 0", n)
	}
}

func TestAPlanThatReturnsNoWriteStoresNoMediaRow(t *testing.T) {
	b, _ := newMediaEnv(t)

	err := b.bank.ApplyImport(ctx, b.exam.ID, func(domain.Bank) (*domain.ImportWrite, error) { return nil, nil })

	if err != nil || mediaRows(t) != 0 {
		t.Errorf("err %v, media rows %d", err, mediaRows(t))
	}
}

func TestFindReturnsOnlyTheStoredHashesAndAnEmptyAnswerIsNotAnError(t *testing.T) { // access failure vs valid empty result
	b, repo := newMediaEnv(t)
	b.applyMedia(t, png(shaOne))

	got, err := repo.Find(ctx, []string{shaOne, shaTwo})
	if err != nil || len(got) != 1 || got[shaOne].SHA256 != shaOne {
		t.Errorf("Find = %v, %v; want only %s", got, err, shaOne)
	}
	none, err := repo.Find(ctx, []string{shaTwo})
	if err != nil || none == nil || len(none) != 0 {
		t.Errorf("Find of an unknown hash = %v, %v; want an empty map", none, err)
	}
	noInput, err := repo.Find(ctx, nil)
	if err != nil || len(noInput) != 0 {
		t.Errorf("Find(nil) = %v, %v", noInput, err)
	}
}

// Phase-2 fix 4: two imports into different exams that share new images and
// reference them in opposite order used to deadlock on the unique index of
// media. The rows are inserted in sha256 order whatever the reference order.

func randomImages(n int) []domain.MediaFile {
	out := make([]domain.MediaFile, n)
	for i := range out {
		sum := sha256.Sum256([]byte(uuid.NewString()))
		out[i] = png(hex.EncodeToString(sum[:]))
	}
	return out
}

func TestConcurrentImportsSharingNewImagesInOppositeOrderNeverDeadlock(t *testing.T) { // fix 4
	for _, tc := range []struct {
		name       string
		images     int
		iterations int
	}{
		{"two shared images", 2, 40},
		{"thirty shared images", 30, 10},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := newMediaEnv(t)
			other := b.repoEnv.add(t, b.subjectID, "Other exam")
			for i := 0; i < tc.iterations; i++ {
				forward := randomImages(tc.images)
				backward := slices.Clone(forward)
				slices.Reverse(backward)

				start := make(chan struct{})
				errs := make(chan error, 2)
				for examID, media := range map[string][]domain.MediaFile{b.exam.ID: forward, other.ID: backward} {
					go func() {
						<-start
						errs <- b.bank.ApplyImport(ctx, examID, func(domain.Bank) (*domain.ImportWrite, error) {
							return &domain.ImportWrite{ExamID: examID, Now: time.Now().UTC(), Media: media}, nil
						})
					}()
				}
				close(start)
				for range 2 {
					if err := <-errs; err != nil {
						t.Fatalf("iteration %d: an import failed: %v", i, err)
					}
				}
				if n := mediaRowsAmong(t, forward); n != tc.images {
					t.Fatalf("iteration %d: %d of %d shared images stored, want all once", i, n, tc.images)
				}
			}
		})
	}
}

func mediaRowsAmong(t *testing.T, files []domain.MediaFile) int {
	t.Helper()
	shas := make([]string, len(files))
	for i, f := range files {
		shas[i] = f.Media.SHA256
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM media WHERE sha256 = ANY($1)`, shas).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}
