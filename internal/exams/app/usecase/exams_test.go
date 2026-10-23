package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
)

var ctx = context.Background()

const (
	subjectA = "0abcdef0-0000-7000-8000-00000000a001"
	subjectB = "0abcdef0-0000-7000-8000-00000000b002"
	deadSub  = "0abcdef0-0000-7000-8000-00000000d003"
)

type env struct {
	repo     *testsupport.FakeExamRepo
	subjects *testsupport.FakeSubjectDirectory
	create   in.CreateExam
	get      in.GetExam
	list     in.ListSubjectExams
	update   in.UpdateExam
	delete   in.DeleteExam
}

func newEnv() env {
	subjects := testsupport.NewFakeSubjectDirectory(subjectA, subjectB) // deadSub is not alive
	repo := testsupport.NewFakeExamRepo(subjects)
	clock := &testsupport.StepClock{}
	ids := &testsupport.SeqIDGen{}
	return env{
		repo: repo, subjects: subjects,
		create: NewCreateExamService(repo, subjects, clock, ids),
		get:    NewGetExamService(repo),
		list:   NewListSubjectExamsService(repo, subjects),
		update: NewUpdateExamService(repo, clock),
		delete: NewDeleteExamService(repo, clock),
	}
}

func (e env) add(t *testing.T, subjectID, name string) domain.ExamSummary {
	t.Helper()
	s, err := e.create.Execute(ctx, in.CreateExamCommand{SubjectID: subjectID, Name: name, QuestionCount: 65, TimeLimitMin: 130, PassPercent: 72})
	if err != nil {
		t.Fatalf("create %q: %v", name, err)
	}
	return s
}

func ptr[T any](v T) *T { return &v }

func wantConflict(t *testing.T, err error, rule string) {
	t.Helper()
	var c *domain.ConflictError
	if !errors.As(err, &c) || c.Rule != rule {
		t.Fatalf("err = %v, want a %s conflict", err, rule)
	}
}

func wantViolation(t *testing.T, err error, field, rule string) {
	t.Helper()
	var v *domain.ValidationError
	if !errors.As(err, &v) {
		t.Fatalf("err = %v, want a validation error on %s/%s", err, field, rule)
	}
	for _, got := range v.Violations {
		if got.Field == field && got.Rule == rule {
			return
		}
	}
	t.Fatalf("violations = %+v, want %s/%s", v.Violations, field, rule)
}

func TestCreateExam(t *testing.T) { // S02-1
	e := newEnv()

	s, err := e.create.Execute(ctx, in.CreateExamCommand{SubjectID: subjectA, Name: " SAA-C03 ", QuestionCount: 65, TimeLimitMin: 130, PassPercent: 72})

	if err != nil {
		t.Fatal(err)
	}
	if s.Name.String() != "SAA-C03" || s.SubjectID != subjectA || s.QuestionCount != 65 || s.TimeLimitMin != 130 ||
		s.PassPercent != 72 || s.Version != 1 || s.QuestionTotal != 0 || s.LastSimulation != nil {
		t.Errorf("summary = %+v", s)
	}
	if _, ok := e.repo.Rows[s.ID]; !ok {
		t.Error("the exam was not persisted")
	}
}

func TestCreateExamRejectsAnAliveNameDifferingOnlyInCaseOrSpaces(t *testing.T) { // S02-2
	e := newEnv()
	e.add(t, subjectA, "SAA-C03")

	_, err := e.create.Execute(ctx, in.CreateExamCommand{SubjectID: subjectA, Name: "  saa-c03 ", QuestionCount: 65, TimeLimitMin: 130, PassPercent: 72})

	wantConflict(t, err, domain.RuleDuplicateName)
}

func TestCreateExamAllowsTheSameNameOnAnotherSubject(t *testing.T) { // S02-2
	e := newEnv()
	e.add(t, subjectA, "SAA-C03")

	e.add(t, subjectB, "SAA-C03")
}

func TestCreateExamReportsOneRangeViolationPerField(t *testing.T) { // S02-3
	e := newEnv()

	_, err := e.create.Execute(ctx, in.CreateExamCommand{SubjectID: subjectA, Name: "X", QuestionCount: 0, TimeLimitMin: 601, PassPercent: 101})

	for _, field := range []string{"questionCount", "timeLimitMin", "passPercent"} {
		wantViolation(t, err, field, "range")
	}
	var v *domain.ValidationError
	errors.As(err, &v)
	if len(v.Violations) != 3 {
		t.Errorf("violations = %+v, want exactly one per field", v.Violations)
	}
	if len(e.repo.Rows) != 0 {
		t.Error("an invalid exam was persisted")
	}
}

func TestCreateExamOnAnUnknownOrDeletedSubjectIsASubjectViolation(t *testing.T) { // S02-4
	e := newEnv()
	for name, subjectID := range map[string]string{"deleted": deadSub, "unknown": "0abcdef0-0000-7000-8000-999999999999"} {
		_, err := e.create.Execute(ctx, in.CreateExamCommand{SubjectID: subjectID, Name: "X", QuestionCount: 65, TimeLimitMin: 130, PassPercent: 72})

		var v *domain.ValidationError
		if !errors.As(err, &v) || len(v.Violations) != 1 || v.Violations[0].Field != "subjectId" || v.Violations[0].Rule != "not_found" {
			t.Errorf("%s subject: err = %v, want exactly subjectId/not_found", name, err)
		}
	}
	if len(e.repo.Rows) != 0 {
		t.Error("an exam was persisted on a missing subject")
	}
}

func TestCreateExamSurfacesDirectoryFailures(t *testing.T) { // an access failure is not "subject not found"
	e := newEnv()
	boom := errors.New("db down")
	e.subjects.Err = boom

	_, err := e.create.Execute(ctx, in.CreateExamCommand{SubjectID: subjectA, Name: "X", QuestionCount: 65, TimeLimitMin: 130, PassPercent: 72})

	if !errors.Is(err, boom) {
		t.Errorf("err = %v, want the directory failure", err)
	}
}

func TestGetExam(t *testing.T) {
	e := newEnv()
	created := e.add(t, subjectA, "SAA-C03")

	got, err := e.get.Execute(ctx, created.ID)

	if err != nil || got.ID != created.ID || got.Name.String() != "SAA-C03" {
		t.Fatalf("got (%+v, %v)", got, err)
	}
	if _, err := e.get.Execute(ctx, "0abcdef0-0000-7000-8000-999999999999"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("unknown id: err = %v, want ErrNotFound", err)
	}
}

func TestListSubjectExamsIsSortedByNameAndScopedToTheSubject(t *testing.T) {
	e := newEnv()
	e.add(t, subjectA, "charlie")
	e.add(t, subjectA, "Bravo")
	e.add(t, subjectA, "alpha")
	e.add(t, subjectB, "Elsewhere")

	list, err := e.list.Execute(ctx, subjectA)

	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 || list[0].Name.String() != "alpha" || list[1].Name.String() != "Bravo" || list[2].Name.String() != "charlie" {
		t.Errorf("list = %+v", list)
	}
}

func TestListSubjectExamsOfASubjectWithoutExamsIsEmpty(t *testing.T) {
	e := newEnv()

	list, err := e.list.Execute(ctx, subjectA)

	if err != nil || list == nil || len(list) != 0 {
		t.Errorf("list = %v, err = %v; want an empty non-nil list", list, err)
	}
}

func TestListSubjectExamsOfAMissingSubjectIsNotFound(t *testing.T) {
	e := newEnv()

	for _, subjectID := range []string{deadSub, "0abcdef0-0000-7000-8000-999999999999"} {
		if _, err := e.list.Execute(ctx, subjectID); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("subject %s: err = %v, want ErrNotFound", subjectID, err)
		}
	}
}

func TestUpdateExamWithTheCurrentVersionIncrementsIt(t *testing.T) { // S02-5
	e := newEnv()
	created := e.add(t, subjectA, "SAA-C03")

	updated, err := e.update.Execute(ctx, created.ID, in.UpdateExamCommand{
		Version: 1, Edit: domain.ExamEdit{Name: ptr("SAA-C04"), PassPercent: ptr(75)},
	})

	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 || updated.Name.String() != "SAA-C04" || updated.PassPercent != 75 ||
		updated.QuestionCount != 65 || updated.TimeLimitMin != 130 {
		t.Errorf("updated = %+v", updated)
	}
	if !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Errorf("updatedAt %v must move past %v", updated.UpdatedAt, created.UpdatedAt)
	}
	stored, _ := e.get.Execute(ctx, created.ID)
	if stored.Version != 2 {
		t.Errorf("stored version = %d, want 2", stored.Version)
	}
}

func TestUpdateExamWithAStaleVersionIsAConflictAndWritesNothing(t *testing.T) { // S02-5
	e := newEnv()
	created := e.add(t, subjectA, "SAA-C03")
	if _, err := e.update.Execute(ctx, created.ID, in.UpdateExamCommand{Version: 1, Edit: domain.ExamEdit{PassPercent: ptr(75)}}); err != nil {
		t.Fatal(err)
	}

	_, err := e.update.Execute(ctx, created.ID, in.UpdateExamCommand{Version: 1, Edit: domain.ExamEdit{PassPercent: ptr(80)}})

	wantConflict(t, err, domain.RuleStaleVersion)
	stored, _ := e.get.Execute(ctx, created.ID)
	if stored.PassPercent != 75 || stored.Version != 2 {
		t.Errorf("stored = %+v; the stale edit must not write", stored)
	}
}

func TestUpdateExamRejectsOutOfRangeNumbers(t *testing.T) { // BR-E2 on edit
	e := newEnv()
	created := e.add(t, subjectA, "SAA-C03")

	_, err := e.update.Execute(ctx, created.ID, in.UpdateExamCommand{Version: 1, Edit: domain.ExamEdit{QuestionCount: ptr(201)}})

	wantViolation(t, err, "questionCount", "range")
}

func TestUpdateExamToAnotherExamsNameIsAConflict(t *testing.T) { // BR-E1 on edit
	e := newEnv()
	e.add(t, subjectA, "SAA-C03")
	other := e.add(t, subjectA, "SAA-C04")

	_, err := e.update.Execute(ctx, other.ID, in.UpdateExamCommand{Version: 1, Edit: domain.ExamEdit{Name: ptr(" saa-c03")}})

	wantConflict(t, err, domain.RuleDuplicateName)
}

func TestUpdateExamNeedsAtLeastOneField(t *testing.T) {
	e := newEnv()
	created := e.add(t, subjectA, "SAA-C03")

	_, err := e.update.Execute(ctx, created.ID, in.UpdateExamCommand{Version: 1})

	wantViolation(t, err, "body", "required")
}

func TestUpdateExamNeedsAPositiveVersion(t *testing.T) {
	e := newEnv()
	created := e.add(t, subjectA, "SAA-C03")

	_, err := e.update.Execute(ctx, created.ID, in.UpdateExamCommand{Version: 0, Edit: domain.ExamEdit{PassPercent: ptr(75)}})

	wantViolation(t, err, "version", "required")
}

func TestUpdateExamOfAnUnknownExamIsNotFound(t *testing.T) {
	e := newEnv()

	_, err := e.update.Execute(ctx, "0abcdef0-0000-7000-8000-999999999999", in.UpdateExamCommand{Version: 1, Edit: domain.ExamEdit{PassPercent: ptr(75)}})

	if !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestDeleteExamIsLogicalAndFreesTheName(t *testing.T) { // S02-6
	e := newEnv()
	created := e.add(t, subjectA, "SAA-C03")

	if err := e.delete.Execute(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	if e.repo.Rows[created.ID].DeletedAt == nil {
		t.Error("the row must remain with deletedAt set")
	}
	if _, err := e.get.Execute(ctx, created.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get after delete: err = %v, want ErrNotFound", err)
	}
	if list, _ := e.list.Execute(ctx, subjectA); len(list) != 0 {
		t.Errorf("list after delete = %+v, want empty", list)
	}
	e.add(t, subjectA, "saa-c03") // the name is reusable
}

func TestDeleteExamTwiceOrUnknownIsNotFound(t *testing.T) {
	e := newEnv()
	created := e.add(t, subjectA, "SAA-C03")
	if err := e.delete.Execute(ctx, created.ID); err != nil {
		t.Fatal(err)
	}

	for _, id := range []string{created.ID, "0abcdef0-0000-7000-8000-999999999999"} {
		if err := e.delete.Execute(ctx, id); !errors.Is(err, domain.ErrNotFound) {
			t.Errorf("delete %s: err = %v, want ErrNotFound", id, err)
		}
	}
}
