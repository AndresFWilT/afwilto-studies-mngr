package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

var ctx = context.Background()

type repoEnv struct {
	repo      *ExamRepository
	subjectID string
	now       time.Time
}

// newRepoEnv wipes the shared tables and seeds one category and one subject
// through SQL: exams never import the studies context.
func newRepoEnv(t *testing.T) *repoEnv {
	t.Helper()
	skipShort(t)
	exec(t, `TRUNCATE categories CASCADE`)
	e := &repoEnv{repo: NewExamRepository(pool), now: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}
	categoryID := uuid.NewString()
	exec(t, `INSERT INTO categories (id, name, color) VALUES ($1, 'TI', '#22D3EE')`, categoryID)
	e.subjectID = e.newSubject(t, categoryID, "AWS")
	return e
}

func (e *repoEnv) newSubject(t *testing.T, categoryID, name string) string {
	t.Helper()
	id := uuid.NewString()
	exec(t, `INSERT INTO subjects (id, category_id, name) VALUES ($1, $2, $3)`, id, categoryID, name)
	return id
}

func (e *repoEnv) otherSubject(t *testing.T, name string) string {
	t.Helper()
	var categoryID string
	if err := pool.QueryRow(ctx, `SELECT category_id FROM subjects WHERE id = $1`, e.subjectID).Scan(&categoryID); err != nil {
		t.Fatal(err)
	}
	return e.newSubject(t, categoryID, name)
}

func (e *repoEnv) exam(t *testing.T, subjectID, name string) domain.Exam {
	t.Helper()
	e.now = e.now.Add(time.Minute)
	exam, err := domain.NewExam(uuid.NewString(), subjectID, name, 65, 130, 72, e.now)
	if err != nil {
		t.Fatal(err)
	}
	return exam
}

func (e *repoEnv) add(t *testing.T, subjectID, name string) domain.Exam {
	t.Helper()
	exam := e.exam(t, subjectID, name)
	if err := e.repo.Create(ctx, exam); err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return exam
}

func wantConflict(t *testing.T, err error, rule string) {
	t.Helper()
	var c *domain.ConflictError
	if !errors.As(err, &c) || c.Rule != rule {
		t.Fatalf("err = %v, want a %s conflict", err, rule)
	}
}

func TestExamRepoCreateAndGetRoundTrip(t *testing.T) { // S02-1 persistence
	e := newRepoEnv(t)
	exam := e.add(t, e.subjectID, "SAA-C03")

	got, err := e.repo.Get(ctx, exam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != exam.ID || got.SubjectID != e.subjectID || got.Name.String() != "SAA-C03" ||
		got.QuestionCount != 65 || got.TimeLimitMin != 130 || got.PassPercent != 72 || got.Version != 1 ||
		!got.CreatedAt.Equal(exam.CreatedAt) || !got.UpdatedAt.Equal(exam.UpdatedAt) || got.DeletedAt != nil {
		t.Errorf("got %+v, want %+v", got, exam)
	}
}

func TestExamRepoGetSummaryHasNoQuestionsOrSimulationYet(t *testing.T) { // slice 02 §1: questionTotal 0, lastSimulation absent
	e := newRepoEnv(t)
	exam := e.add(t, e.subjectID, "SAA-C03")

	got, err := e.repo.GetSummary(ctx, exam.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != exam.ID || got.Name.String() != "SAA-C03" || got.QuestionTotal != 0 || got.LastSimulation != nil {
		t.Errorf("summary = %+v", got)
	}
}

func TestExamRepoUnknownExamIsNotFound(t *testing.T) {
	e := newRepoEnv(t)

	if _, err := e.repo.Get(ctx, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get = %v, want ErrNotFound", err)
	}
	if _, err := e.repo.GetSummary(ctx, uuid.NewString()); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("GetSummary = %v, want ErrNotFound", err)
	}
}

func TestExamRepoDuplicateNameIgnoresCaseWithinASubject(t *testing.T) { // S02-2, BR-E1
	e := newRepoEnv(t)
	e.add(t, e.subjectID, "SAA-C03")

	wantConflict(t, e.repo.Create(ctx, e.exam(t, e.subjectID, "saa-c03")), domain.RuleDuplicateName)
}

func TestExamRepoDuplicateNameIsScopedToTheSubject(t *testing.T) { // S02-2
	e := newRepoEnv(t)
	e.add(t, e.subjectID, "SAA-C03")

	e.add(t, e.otherSubject(t, "Azure"), "SAA-C03")
}

func TestExamRepoCreateOnAnUnknownOrDeletedSubjectIsASubjectViolation(t *testing.T) { // S02-4 (race backstop)
	e := newRepoEnv(t)
	exec(t, `UPDATE subjects SET deleted_at = now() WHERE id = $1`, e.subjectID)

	for name, subjectID := range map[string]string{"unknown": uuid.NewString(), "deleted": e.subjectID} {
		err := e.repo.Create(ctx, e.exam(t, subjectID, "X"))
		var v *domain.ValidationError
		if !errors.As(err, &v) || v.Violations[0].Field != "subjectId" || v.Violations[0].Rule != "not_found" {
			t.Errorf("%s subject: err = %v, want subjectId/not_found", name, err)
		}
	}
}

func TestExamRepoListIsSortedByNameCaseInsensitiveThenAliveOnly(t *testing.T) { // slice 02 §1 list
	e := newRepoEnv(t)
	e.add(t, e.subjectID, "charlie")
	e.add(t, e.subjectID, "Bravo")
	e.add(t, e.subjectID, "alpha")
	gone := e.add(t, e.subjectID, "Zulu")
	e.add(t, e.otherSubject(t, "Azure"), "Other subject")
	gone.Delete(e.now)
	if err := e.repo.Delete(ctx, gone); err != nil {
		t.Fatal(err)
	}

	list, err := e.repo.ListBySubject(ctx, e.subjectID)
	if err != nil {
		t.Fatal(err)
	}

	var names []string
	for _, s := range list {
		names = append(names, s.Name.String())
	}
	if len(names) != 3 || names[0] != "alpha" || names[1] != "Bravo" || names[2] != "charlie" {
		t.Errorf("names = %v, want [alpha Bravo charlie]", names)
	}
}

func TestExamRepoListOfASubjectWithoutExamsIsEmptyNotNil(t *testing.T) {
	e := newRepoEnv(t)

	list, err := e.repo.ListBySubject(ctx, e.subjectID)
	if err != nil || list == nil || len(list) != 0 {
		t.Errorf("list = %v, %v; want an empty non-nil slice", list, err)
	}
}

func TestExamRepoUpdateAppliesTheEditAtTheExpectedVersion(t *testing.T) { // S02-5
	e := newRepoEnv(t)
	exam := e.add(t, e.subjectID, "SAA-C03")
	if err := exam.Edit(domain.ExamEdit{Name: ptr("SAA-C04"), PassPercent: ptr(75)}, e.now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	if err := e.repo.Update(ctx, exam, 1); err != nil {
		t.Fatal(err)
	}

	got, _ := e.repo.Get(ctx, exam.ID)
	if got.Name.String() != "SAA-C04" || got.PassPercent != 75 || got.Version != 2 || !got.UpdatedAt.Equal(exam.UpdatedAt) {
		t.Errorf("got %+v", got)
	}
}

func ptr[T any](v T) *T { return &v }

func TestExamRepoUpdateWithAStaleVersionIsAConflict(t *testing.T) { // S02-5
	e := newRepoEnv(t)
	exam := e.add(t, e.subjectID, "SAA-C03")
	_ = exam.Edit(domain.ExamEdit{PassPercent: ptr(75)}, e.now)

	wantConflict(t, e.repo.Update(ctx, exam, 7), domain.RuleStaleVersion)

	got, _ := e.repo.Get(ctx, exam.ID)
	if got.Version != 1 || got.PassPercent != 72 {
		t.Errorf("a rejected update must not write, got %+v", got)
	}
}

func TestExamRepoUpdateOfADeletedExamIsNotFound(t *testing.T) {
	e := newRepoEnv(t)
	exam := e.add(t, e.subjectID, "SAA-C03")
	exam.Delete(e.now)
	if err := e.repo.Delete(ctx, exam); err != nil {
		t.Fatal(err)
	}

	if err := e.repo.Update(ctx, exam, 1); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Update = %v, want ErrNotFound", err)
	}
}

func TestExamRepoUpdateToAnExistingNameIsAConflict(t *testing.T) { // BR-E1 on edit
	e := newRepoEnv(t)
	e.add(t, e.subjectID, "SAA-C03")
	other := e.add(t, e.subjectID, "SAA-C04")
	_ = other.Edit(domain.ExamEdit{Name: ptr("saa-c03")}, e.now)

	wantConflict(t, e.repo.Update(ctx, other, 1), domain.RuleDuplicateName)
}

func TestExamRepoUpdateKeepingTheOwnNameIsNotAConflict(t *testing.T) { // BR-E1: the row does not clash with itself
	e := newRepoEnv(t)
	exam := e.add(t, e.subjectID, "SAA-C03")
	_ = exam.Edit(domain.ExamEdit{Name: ptr("saa-c03"), TimeLimitMin: ptr(140)}, e.now)

	if err := e.repo.Update(ctx, exam, 1); err != nil {
		t.Fatal(err)
	}
}

func TestExamRepoDeleteIsLogicalAndFreesTheName(t *testing.T) { // S02-6
	e := newRepoEnv(t)
	exam := e.add(t, e.subjectID, "SAA-C03")
	exam.Delete(e.now.Add(time.Hour))

	if err := e.repo.Delete(ctx, exam); err != nil {
		t.Fatal(err)
	}

	if _, err := e.repo.Get(ctx, exam.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
	var stored int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM exams WHERE id = $1 AND deleted_at IS NOT NULL`, exam.ID).Scan(&stored); err != nil || stored != 1 {
		t.Errorf("the row must remain with deleted_at set, count = %d, err = %v", stored, err)
	}
	if err := e.repo.Delete(ctx, exam); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("second Delete = %v, want ErrNotFound", err)
	}
	e.add(t, e.subjectID, "saa-c03") // the name is reusable
}

func TestExamsTableRejectsHardDelete(t *testing.T) { // S02-8
	e := newRepoEnv(t)
	exam := e.add(t, e.subjectID, "SAA-C03")

	_, err := pool.Exec(ctx, `DELETE FROM exams WHERE id = $1`, exam.ID)

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		t.Fatalf("DELETE error = %v, want a PgError", err)
	}
	if pgErr.Code != "23001" {
		t.Errorf("SQLSTATE = %s, want 23001 (restrict_violation)", pgErr.Code)
	}
	if _, err := e.repo.Get(ctx, exam.ID); err != nil {
		t.Errorf("the exam must survive a rejected DELETE: %v", err)
	}
}

func TestExamsTableChecksRangesWhateverTheWriter(t *testing.T) { // BR-E2 in the database
	e := newRepoEnv(t)
	for name, values := range map[string]string{
		"questionCount 0":   `0, 130, 72`,
		"questionCount 201": `201, 130, 72`,
		"timeLimitMin 601":  `65, 601, 72`,
		"timeLimitMin 0":    `65, 0, 72`,
		"passPercent 101":   `65, 130, 101`,
		"passPercent 0":     `65, 130, 0`,
	} {
		_, err := pool.Exec(ctx, `INSERT INTO exams (id, subject_id, name, question_count, time_limit_min, pass_percent)
			VALUES ($1, $2, 'X', `+values+`)`, uuid.NewString(), e.subjectID)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
			t.Errorf("%s: err = %v, want SQLSTATE 23514 (check_violation)", name, err)
		}
	}
}

func TestExamsTableRequiresAnExistingSubject(t *testing.T) {
	e := newRepoEnv(t)
	_ = e

	_, err := pool.Exec(ctx, `INSERT INTO exams (id, subject_id, name, question_count, time_limit_min, pass_percent)
		VALUES ($1, $2, 'X', 65, 130, 72)`, uuid.NewString(), uuid.NewString())

	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23503" {
		t.Errorf("err = %v, want SQLSTATE 23503 (foreign_key_violation)", err)
	}
}
