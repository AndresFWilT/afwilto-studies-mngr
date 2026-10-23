package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// Migration 0011 closes the race between a subject delete and an exam create
// (phase-1 review H1). Each race test holds one side in an open transaction
// and runs the other side against the pool: with the guard the second writer
// waits for the shared advisory lock, then sees the committed outcome.

const raceWait = 300 * time.Millisecond

func insertExamSQL(id, subjectID string) (string, []any) {
	return `INSERT INTO exams (id, subject_id, name, question_count, time_limit_min, pass_percent)
		VALUES ($1, $2, 'Racer', 65, 130, 72)`, []any{id, subjectID}
}

func TestSubjectDeleteWaitsForAnUncommittedExamInsertAndThenIsRefusedAsInUse(t *testing.T) { // H1, S02-7 race half
	e := newRepoEnv(t)
	txA, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = txA.Rollback(ctx) }()
	examID := uuid.NewString()
	sql, args := insertExamSQL(examID, e.subjectID)
	if _, err := txA.Exec(ctx, sql, args...); err != nil {
		t.Fatal(err)
	}

	deleted := make(chan error, 1)
	go func() {
		_, err := pool.Exec(ctx, `UPDATE subjects SET deleted_at = now() WHERE id = $1`, e.subjectID)
		deleted <- err
	}()
	select {
	case err := <-deleted:
		t.Fatalf("the subject delete finished (err %v) while an exam insert was uncommitted; it must wait for the guard lock", err)
	case <-time.After(raceWait):
	}
	if err := txA.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-deleted:
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "subject_in_use" {
			t.Fatalf("subject delete after the exam committed = %v, want check_violation subject_in_use", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the subject delete never finished")
	}
	assertNoAliveExamOnADeletedSubject(t)
}

func TestExamCreateWaitsForAnUncommittedSubjectDeleteAndThenReportsTheSubjectNotFound(t *testing.T) { // H1, race other half
	e := newRepoEnv(t)
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = txB.Rollback(ctx) }()
	if _, err := txB.Exec(ctx, `UPDATE subjects SET deleted_at = now() WHERE id = $1`, e.subjectID); err != nil {
		t.Fatal(err)
	}

	created := make(chan error, 1)
	go func() { created <- e.repo.Create(ctx, e.exam(t, e.subjectID, "SAA-C03")) }()
	select {
	case err := <-created:
		t.Fatalf("the exam create finished (err %v) while a subject delete was uncommitted; it must wait for the guard lock", err)
	case <-time.After(raceWait):
	}
	if err := txB.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-created:
		var v *domain.ValidationError
		if !errors.As(err, &v) || len(v.Violations) != 1 || v.Violations[0].Field != "subjectId" || v.Violations[0].Rule != "not_found" {
			t.Fatalf("exam create after the subject was deleted = %v, want a not_found violation on subjectId", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the exam create never finished")
	}
	assertNoAliveExamOnADeletedSubject(t)
}

func TestTheDatabaseRejectsAnExamOnADeletedSubjectEvenOutsideTheRepository(t *testing.T) { // H1, defence in depth
	e := newRepoEnv(t)
	exec(t, `UPDATE subjects SET deleted_at = now() WHERE id = $1`, e.subjectID)

	sql, args := insertExamSQL(uuid.NewString(), e.subjectID)
	_, err := pool.Exec(ctx, sql, args...)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" || pgErr.ConstraintName != "exam_subject_not_found" {
		t.Fatalf("insert on a deleted subject = %v, want check_violation exam_subject_not_found", err)
	}
}

func TestRenamingAnExamDoesNotWaitForTheSubjectGuardLock(t *testing.T) { // H1: only inserts and subject moves take the lock
	e := newRepoEnv(t)
	exam := e.add(t, e.subjectID, "SAA-C03")
	txB, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = txB.Rollback(ctx) }()
	other := e.otherSubject(t, "Azure")
	if _, err := txB.Exec(ctx, `UPDATE subjects SET deleted_at = now() WHERE id = $1`, other); err != nil {
		t.Fatal(err) // takes the guard lock and holds it
	}

	renamed := make(chan error, 1)
	go func() {
		_, err := pool.Exec(ctx, `UPDATE exams SET name = 'SAA-C03 v2', updated_at = now() WHERE id = $1`, exam.ID)
		renamed <- err
	}()
	select {
	case err := <-renamed:
		if err != nil {
			t.Fatalf("rename: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a plain exam edit waited for the subject guard lock")
	}
}

func assertNoAliveExamOnADeletedSubject(t *testing.T) {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM exams e JOIN subjects s ON s.id = e.subject_id
		WHERE e.deleted_at IS NULL AND s.deleted_at IS NOT NULL`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("%d alive exams hang off a deleted subject: both writers succeeded", n)
	}
}

func triggerExists(t *testing.T, table, name string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM pg_trigger WHERE tgrelid = $1::regclass AND tgname = $2)`, table, name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestMigration0011AddsTheGuardTriggersAndDownRemovesThem(t *testing.T) { // H1
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
	if !triggerExists(t, "subjects", "subjects_guard_exams") || !triggerExists(t, "exams", "exams_guard_subject") {
		t.Fatal("guard triggers missing after migrate up")
	}
	if err := m.Migrate(10); err != nil {
		t.Fatalf("migrate down to version 10: %v", err)
	}
	if triggerExists(t, "subjects", "subjects_guard_exams") || triggerExists(t, "exams", "exams_guard_subject") {
		t.Error("guard triggers still present after rolling back to version 10")
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if !triggerExists(t, "subjects", "subjects_guard_exams") {
		t.Error("guard trigger missing after re-applying")
	}
}
