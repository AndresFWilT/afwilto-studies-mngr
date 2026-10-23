package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// Phase-1 review M1: the bank and the exam summary are read in one snapshot.
// Each test parks the read on a table lock part-way, commits a new question
// from another connection, and lets the read finish. Read statement by
// statement the second half would see the new question and the first half
// would not.

// holdTable locks a table against every reader and writer until release.
func holdTable(t *testing.T, table string) (release func()) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, "LOCK TABLE "+table+" IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	released := false
	t.Cleanup(func() {
		if !released {
			_ = tx.Rollback(context.Background())
		}
	})
	return func() {
		released = true
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

// waitForALockWait returns once a backend waits on a lock, i.e. the read is
// parked on the held table.
func waitForALockWait(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database()`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the read never parked on the held table lock")
}

// addQuestionBehindTheReader inserts a question linked to a category through
// plain SQL, as a concurrent writer that does not hold the exam lock.
func addQuestionBehindTheReader(t *testing.T, examID, categoryID string) {
	t.Helper()
	id := uuid.NewString()
	exec(t, `INSERT INTO questions (id, exam_id, key, type, stem, source_kind, source_name, stem_fingerprint)
		VALUES ($1, $2, 'late', 'single', 'late', 'own', 'x', 'late')`, id, examID)
	exec(t, `INSERT INTO question_categories (question_id, category_id) VALUES ($1, $2)`, id, categoryID)
}

func categoryID(t *testing.T, name string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `SELECT id FROM exam_categories WHERE name = $1 AND deleted_at IS NULL`, name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestLoadBankReadsOneSnapshotEvenWhenAQuestionIsAddedMidRead(t *testing.T) { // M1
	b := newBankEnv(t)
	b.apply(t, fileOf(fq("q1", cn("Dominio", "Seguridad"))))
	cat := categoryID(t, "Seguridad")
	release := holdTable(t, "question_options")

	done := make(chan domain.Bank, 1)
	go func() {
		bank, err := b.bank.LoadBank(ctx, b.exam.ID)
		if err != nil {
			t.Error(err)
		}
		done <- bank
	}()
	waitForALockWait(t)
	addQuestionBehindTheReader(t, b.exam.ID, cat)
	release()

	bank := <-done
	if len(bank.Questions) != 1 || len(bank.Questions[0].Categories) != 1 {
		t.Errorf("bank has %d questions and the first has %d category links; want 1 and 1: the late question's link must not leak into q1", len(bank.Questions), len(bank.Questions[0].Categories))
	}
}

func TestGetSummaryReadsOneSnapshotEvenWhenAQuestionIsAddedMidRead(t *testing.T) { // M1
	b := newBankEnv(t)
	b.apply(t, fileOf(fq("q1", cn("Dominio", "Seguridad"))))
	cat := categoryID(t, "Seguridad")
	release := holdTable(t, "exam_classifications")

	done := make(chan domain.ExamSummary, 1)
	go func() {
		summary, err := b.exams.GetSummary(ctx, b.exam.ID)
		if err != nil {
			t.Error(err)
		}
		done <- summary
	}()
	waitForALockWait(t)
	addQuestionBehindTheReader(t, b.exam.ID, cat)
	release()

	summary := <-done
	inCategories := 0
	for _, c := range summary.Classifications {
		for _, k := range c.Categories {
			inCategories += k.QuestionCount
		}
	}
	if summary.QuestionTotal != inCategories {
		t.Errorf("questionTotal %d but the categories hold %d questions: the two halves read different snapshots", summary.QuestionTotal, inCategories)
	}
}

func TestApplyImportIgnoresRowsThatBelongToNoLoadedQuestion(t *testing.T) { // M1: orphans are skipped, never attached to element 0
	b := newBankEnv(t)
	b.apply(t, fileOf(fq("q1", cn("Dominio", "Seguridad")), fq("q2", cn("Dominio", "Seguridad"))))
	exec(t, `UPDATE questions SET deleted_at = now() WHERE key = 'q2'`)
	release := holdTable(t, "question_options")

	var seen domain.Bank
	done := make(chan error, 1)
	go func() {
		done <- b.bank.ApplyImport(ctx, b.exam.ID, func(bank domain.Bank) (*domain.ImportWrite, error) {
			seen = bank
			return nil, nil
		})
	}()
	waitForALockWait(t)
	exec(t, `UPDATE questions SET deleted_at = NULL WHERE key = 'q2'`) // a restore needs no exam lock, so it lands mid-read
	release()

	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(seen.Questions) != 1 || len(seen.Questions[0].Options) != 3 || len(seen.Questions[0].Categories) != 1 {
		t.Errorf("planned against %d questions; the first has %d options and %d links, want 1 question with 3 and 1: the restored q2's rows belong to no loaded question",
			len(seen.Questions), len(seen.Questions[0].Options), len(seen.Questions[0].Categories))
	}
}
