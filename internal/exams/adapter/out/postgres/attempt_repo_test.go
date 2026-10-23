package postgres

import (
	"errors"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// ── migration 0013 ──────────────────────────────────────────────────────────

func TestMigration0013CreatesTheAttemptTablesWithTheSpecColumns(t *testing.T) { // slice 06a §3 and its amendment
	skipShort(t)
	required := map[string][]string{
		"attempts": {"id", "exam_id", "config", "status", "seed", "started_at", "deadline_at", "submitted_at", "score_percent", "passed",
			"suggested_minutes", "pass_percent"},
		"attempt_items": {"attempt_id", "position", "question_id", "snapshot", "selected", "flagged", "answered_at", "revealed", "correct"},
	}
	for table, cols := range required {
		have := columnsOf(t, table)
		for _, c := range cols {
			if !slices.Contains(have, c) {
				t.Errorf("%s lacks column %q (has %v)", table, c, have)
			}
		}
	}
}

func TestMigration0013GuardsBothTablesAgainstHardDeletes(t *testing.T) { // slice 06a §3
	skipShort(t)
	for _, table := range []string{"attempts", "attempt_items"} {
		var n int
		if err := pool.QueryRow(ctx,
			`SELECT count(*) FROM information_schema.triggers WHERE event_object_table = $1 AND event_manipulation = 'DELETE'`, table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("DELETE triggers on %s = %d, want 1 (forbid_hard_delete)", table, n)
		}
	}
}

func TestMigration0013HardDeletesFailAtRuntime(t *testing.T) {
	e := newAttemptEnv(t)
	a := e.start(t, 3)
	for _, stmt := range []string{`DELETE FROM attempt_items WHERE attempt_id = $1`, `DELETE FROM attempts WHERE id = $1`} {
		_, err := pool.Exec(ctx, stmt, a.ID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23001" { // restrict_violation
			t.Errorf("%s: err = %v, want restrict_violation", stmt, err)
		}
	}
}

func TestMigration0013DownDropsTheAttemptTablesAndUpRestoresThem(t *testing.T) {
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
	if !tableExists(t, "attempts") || !tableExists(t, "attempt_items") {
		t.Fatal("attempt tables missing after migrate up")
	}
	if err := m.Migrate(12); err != nil {
		t.Fatalf("migrate down to version 12: %v", err)
	}
	if tableExists(t, "attempts") || tableExists(t, "attempt_items") {
		t.Error("attempt tables still present after rolling back to version 12")
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if !tableExists(t, "attempts") {
		t.Error("attempts missing after re-applying the migration")
	}
}

// insertRawAttempt writes a header row straight through SQL.
func insertRawAttempt(examID, status string) error {
	_, err := pool.Exec(ctx, `
		INSERT INTO attempts (id, exam_id, config, status, seed, started_at, suggested_minutes, pass_percent)
		VALUES ($1, $2, '{"selection":{"kind":"simulation"}}', $3, 1, now(), 130, 72)`, uuid.NewString(), examID, status)
	return err
}

func TestOneInProgressAttemptPerExamIsEnforcedByTheDatabase(t *testing.T) { // BR-A1, slice 06a §3
	e := newAttemptEnv(t)
	other := e.add(t, e.subjectID, "AIF-C01")

	if err := insertRawAttempt(e.exam.ID, "in_progress"); err != nil {
		t.Fatal(err)
	}
	err := insertRawAttempt(e.exam.ID, "in_progress")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation || pgErr.ConstraintName != "attempts_exam_in_progress_ux" {
		t.Fatalf("second open attempt: err = %v, want unique violation on attempts_exam_in_progress_ux", err)
	}
	if err := insertRawAttempt(other.ID, "in_progress"); err != nil {
		t.Errorf("an open attempt on another exam must be allowed: %v", err)
	}
	for _, status := range []string{"submitted", "submitted", "expired"} {
		if err := insertRawAttempt(e.exam.ID, status); err != nil {
			t.Errorf("closed attempts are not limited, got %v for %s", err, status)
		}
	}
	exec(t, `UPDATE attempts SET status = 'submitted' WHERE exam_id = $1 AND status = 'in_progress'`, e.exam.ID)
	if err := insertRawAttempt(e.exam.ID, "in_progress"); err != nil {
		t.Errorf("after the first closed a new one must be allowed: %v", err)
	}
}

func TestAttemptsTableChecksItsStatusWhateverTheWriter(t *testing.T) {
	e := newAttemptEnv(t)
	if err := insertRawAttempt(e.exam.ID, "paused"); err == nil {
		t.Error("an unknown status was accepted")
	}
}

// ── repository ──────────────────────────────────────────────────────────────

type attemptEnv struct {
	*bankEnv
	attempts *AttemptRepository
	n        int
}

// newAttemptEnv: an exam with the three questions of awsFile.
func newAttemptEnv(t *testing.T) *attemptEnv {
	t.Helper()
	b := newBankEnv(t)
	b.apply(t, awsFile())
	return &attemptEnv{bankEnv: b, attempts: NewAttemptRepository(pool)}
}

func untimedRandom(count int) domain.SessionConfig {
	return domain.SessionConfig{
		Selection: domain.Selection{Kind: domain.SelectRandom, Count: count},
		Timing:    &domain.Timing{Kind: domain.Untimed}, Feedback: domain.FeedbackEnd,
	}
}

// build draws an attempt for the exam without storing it.
func (e *attemptEnv) build(t *testing.T, examID string, cfg domain.SessionConfig, seed uint64) domain.Attempt {
	t.Helper()
	exam, err := e.exams.Get(ctx, examID)
	if err != nil {
		t.Fatal(err)
	}
	bank, err := e.bank.LoadBank(ctx, examID)
	if err != nil {
		t.Fatal(err)
	}
	e.n++
	a, err := domain.StartAttempt(uuid.NewString(), exam, bank, cfg, seed, time.Date(2026, 10, 23, 9, e.n, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// start draws and stores an attempt of count questions.
func (e *attemptEnv) start(t *testing.T, count int) domain.Attempt {
	t.Helper()
	a := e.build(t, e.exam.ID, untimedRandom(count), 5)
	if err := e.attempts.Create(ctx, a); err != nil {
		t.Fatalf("create: %v", err)
	}
	return a
}

func TestAttemptRepoCreateRoundTripsHeaderItemsAndSnapshot(t *testing.T) { // slice 06a §4 "snapshot round-trip"
	e := newAttemptEnv(t)
	cfg := domain.SessionConfig{
		Selection: domain.Selection{Kind: domain.SelectRandom, Count: 3},
		Timing:    &domain.Timing{Kind: domain.Timed, Minutes: 25}, Feedback: domain.FeedbackEach,
	}
	want := e.build(t, e.exam.ID, cfg, 42)

	if err := e.attempts.Create(ctx, want); err != nil {
		t.Fatal(err)
	}
	got, err := loadAttempt(ctx, pool, want.ID)
	if err != nil {
		t.Fatal(err)
	}

	if len(got.Items) != 3 {
		t.Fatalf("items = %d, want 3", len(got.Items))
	}
	if !reflect.DeepEqual(got, normalizeAttempt(want)) {
		t.Errorf("round trip changed the attempt:\n got %+v\nwant %+v", got, normalizeAttempt(want))
	}
	var origin *domain.Origin
	for _, it := range got.Items {
		if it.Snapshot.Origin != nil {
			origin = it.Snapshot.Origin
		}
	}
	if origin == nil {
		t.Error("no snapshot kept its origin (awsFile has two with one)")
	}
}

// normalizeAttempt maps the in-memory attempt to what the database returns:
// times in UTC and the empty answer state as non-nil empty selections.
func normalizeAttempt(a domain.Attempt) domain.Attempt {
	a.StartedAt = a.StartedAt.UTC()
	if a.DeadlineAt != nil {
		d := a.DeadlineAt.UTC()
		a.DeadlineAt = &d
	}
	items := make([]domain.AttemptItem, len(a.Items))
	for i, it := range a.Items {
		if it.Selected == nil {
			it.Selected = []string{}
		}
		items[i] = it
	}
	a.Items = items
	return a
}

func TestAttemptRepoStoresTheSeedAsABitPattern(t *testing.T) { // slice 06a §5: uint64 on the wire, bigint in the column
	for _, seed := range []uint64{0, 1, 1<<63 - 1, 1 << 63, 18446744073709551615} {
		e := newAttemptEnv(t)
		a := e.build(t, e.exam.ID, untimedRandom(1), seed)
		if err := e.attempts.Create(ctx, a); err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		got, err := loadAttempt(ctx, pool, a.ID)
		if err != nil || got.Seed != seed {
			t.Errorf("seed %d came back as %d (err %v)", seed, got.Seed, err)
		}
	}
}

func TestAttemptRepoSecondCreateIsAnAttemptInProgressConflictWithTheOpenId(t *testing.T) { // S06a-6, BR-A1
	e := newAttemptEnv(t)
	first := e.start(t, 2)

	err := e.attempts.Create(ctx, e.build(t, e.exam.ID, untimedRandom(2), 6))

	wantConflict(t, err, domain.RuleAttemptInProgress)
	c := err.(*domain.ConflictError)
	if c.Field != "attemptId" || !strings.Contains(c.Message, first.ID) {
		t.Errorf("conflict = %+v, want attemptId and %s in the message", c, first.ID)
	}
	var attempts, items int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM attempts`).Scan(&attempts)
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM attempt_items`).Scan(&items)
	if attempts != 1 || items != 2 {
		t.Errorf("rows after the conflict: %d attempts, %d items; want 1 and 2 (the loser leaves nothing behind)", attempts, items)
	}
}

func TestAttemptRepoConcurrentStartsLeaveExactlyOneOpenAttempt(t *testing.T) { // BR-A1 under a race
	e := newAttemptEnv(t)
	const racers = 8
	attempts := make([]domain.Attempt, racers)
	for i := range attempts {
		attempts[i] = e.build(t, e.exam.ID, untimedRandom(2), uint64(i))
	}
	errs := make([]error, racers)
	var wg sync.WaitGroup
	for i := range attempts {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = e.attempts.Create(ctx, attempts[i])
		}()
	}
	wg.Wait()

	won := 0
	for _, err := range errs {
		var c *domain.ConflictError
		switch {
		case err == nil:
			won++
		case errors.As(err, &c) && c.Rule == domain.RuleAttemptInProgress:
		default:
			t.Errorf("unexpected error %v", err)
		}
	}
	if won != 1 {
		t.Errorf("%d starts succeeded, want exactly 1", won)
	}
}

func TestAttemptRepoCreateOnAnUnknownOrDeletedExamIsNotFound(t *testing.T) {
	e := newAttemptEnv(t)
	a := e.build(t, e.exam.ID, untimedRandom(1), 1)
	ghost := a
	ghost.ExamID = uuid.NewString()
	if err := e.attempts.Create(ctx, ghost); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown exam: err = %v, want ErrNotFound", err)
	}

	exec(t, `UPDATE questions SET deleted_at = now() WHERE exam_id = $1`, e.exam.ID)
	victim := e.exam
	victim.Delete(time.Now())
	if err := e.exams.Delete(ctx, victim); err != nil {
		t.Fatal(err)
	}
	if err := e.attempts.Create(ctx, a); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("deleted exam: err = %v, want ErrNotFound", err)
	}
}

func TestAttemptRepoFailedItemInsertLeavesNoAttemptBehind(t *testing.T) { // whole-attempt write
	e := newAttemptEnv(t)
	a := e.build(t, e.exam.ID, untimedRandom(2), 1)
	a.Items[1].QuestionID = uuid.NewString() // no such question: the foreign key refuses

	if err := e.attempts.Create(ctx, a); err == nil {
		t.Fatal("expected the item insert to fail")
	}

	var n int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM attempts`).Scan(&n)
	if n != 0 {
		t.Errorf("%d attempt rows after a failed create, want 0", n)
	}
	if _, found, _ := e.attempts.FindInProgress(ctx, e.exam.ID); found {
		t.Error("the failed create left the exam with an open attempt")
	}
}

func TestAttemptRepoFindInProgress(t *testing.T) {
	e := newAttemptEnv(t)
	other := e.add(t, e.subjectID, "AIF-C01")

	if id, found, err := e.attempts.FindInProgress(ctx, e.exam.ID); err != nil || found || id != "" {
		t.Errorf("no attempt yet: %q %v %v, want none and no error", id, found, err)
	}
	a := e.start(t, 1)
	if id, found, err := e.attempts.FindInProgress(ctx, e.exam.ID); err != nil || !found || id != a.ID {
		t.Errorf("open attempt: %q %v %v, want %s", id, found, err, a.ID)
	}
	if _, found, _ := e.attempts.FindInProgress(ctx, other.ID); found {
		t.Error("another exam reported an open attempt")
	}
	exec(t, `UPDATE attempts SET status = 'submitted' WHERE id = $1`, a.ID)
	if _, found, _ := e.attempts.FindInProgress(ctx, e.exam.ID); found {
		t.Error("a submitted attempt counts as open")
	}
}

func TestTheSnapshotSurvivesLaterEditsAndDeletesOfTheQuestion(t *testing.T) { // AC-8 for attempts
	e := newAttemptEnv(t)
	a := e.start(t, 3)
	before, _ := loadAttempt(ctx, pool, a.ID)

	exec(t, `UPDATE questions SET stem = 'rewritten', version = version + 1 WHERE exam_id = $1`, e.exam.ID)
	exec(t, `UPDATE question_options SET deleted_at = now() WHERE question_id IN (SELECT id FROM questions WHERE exam_id = $1)`, e.exam.ID)
	exec(t, `UPDATE questions SET deleted_at = now() WHERE exam_id = $1`, e.exam.ID)
	after, err := loadAttempt(ctx, pool, a.ID)

	if err != nil || !reflect.DeepEqual(before, after) {
		t.Errorf("the attempt changed with its questions (err %v):\nbefore %+v\nafter  %+v", err, before, after)
	}
	if strings.Contains(after.Items[0].Snapshot.Stem, "rewritten") {
		t.Error("the snapshot follows the live question")
	}
}
