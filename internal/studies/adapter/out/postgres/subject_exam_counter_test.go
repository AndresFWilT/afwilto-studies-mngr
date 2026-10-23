package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/afwilto/studies-mngr/internal/studies/app/usecase"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
	"github.com/afwilto/studies-mngr/internal/studies/testsupport"
)

// insertExam writes an exam row through SQL: studies never import the exams
// context (Spec 18 D-10), so the fixture is raw.
func insertExam(t *testing.T, id, subjectID, name string, deleted bool) {
	t.Helper()
	deletedAt := "NULL"
	if deleted {
		deletedAt = "now()"
	}
	exec(t, fmt.Sprintf(`INSERT INTO exams (id, subject_id, name, question_count, time_limit_min, pass_percent, deleted_at)
		VALUES ('%s', '%s', '%s', 65, 130, 72, %s)`, id, subjectID, name, deletedAt))
}

func TestSubjectExamCounterCountsOnlyAliveExamsOfTheSubject(t *testing.T) { // S02-7 (BR-E3 subject half)
	e := newSubjRepoEnv(t)
	cat := e.cats.add(t, "TI", nil)
	aws, azure := e.add(t, cat.ID, "AWS"), e.add(t, cat.ID, "Azure")
	insertExam(t, e.ids.NewID(), aws.ID, "SAA-C03", false)
	insertExam(t, e.ids.NewID(), aws.ID, "SAP-C02", false)
	insertExam(t, e.ids.NewID(), aws.ID, "Retired", true)
	insertExam(t, e.ids.NewID(), azure.ID, "AZ-104", false)
	counter := NewSubjectExamCounter(pool)

	for name, tc := range map[string]struct {
		subjectID string
		want      int
	}{
		"two alive, one deleted": {aws.ID, 2},
		"another subject":        {azure.ID, 1},
		"no exams":               {e.add(t, cat.ID, "GCP").ID, 0},
	} {
		got, err := counter.CountAliveExams(context.Background(), tc.subjectID)
		if err != nil || got != tc.want {
			t.Errorf("%s: CountAliveExams = (%d, %v), want %d", name, got, err, tc.want)
		}
	}
}

func TestSubjectRepoRefusesToDeleteASubjectWithAnAliveExamWhateverTheServiceChecked(t *testing.T) { // S02-7 gap, H1: the DB is the guard
	e := newSubjRepoEnv(t)
	cat := e.cats.add(t, "TI", nil)
	aws := e.add(t, cat.ID, "AWS")
	insertExam(t, e.ids.NewID(), aws.ID, "SAA-C03", false)

	aws.Delete(e.now.Add(time.Hour))
	err := e.repo.Update(context.Background(), aws)

	wantConflictErr(t, err, domain.RuleInUse)
	var c *domain.ConflictError
	if errors.As(err, &c) && strings.Contains(strings.ToLower(c.Message), "notes") {
		t.Errorf("the message %q names notes, but the blocker here is an exam", c.Message)
	}
	if _, getErr := e.repo.Get(context.Background(), aws.ID); getErr != nil {
		t.Errorf("the subject must stay alive: %v", getErr)
	}
}

func TestDeleteSubjectServiceOverRealAdaptersIsBlockedByAnAliveExam(t *testing.T) { // S02-7 gap: the real path, no fakes
	e := newSubjRepoEnv(t)
	cat := e.cats.add(t, "TI", nil)
	aws := e.add(t, cat.ID, "AWS")
	insertExam(t, e.ids.NewID(), aws.ID, "SAA-C03", false)
	svc := usecase.NewDeleteSubjectService(e.repo, NewNoteRepository(pool), NewSubjectExamCounter(pool), &testsupport.StepClock{})

	wantConflictErr(t, svc.Execute(context.Background(), aws.ID), domain.RuleInUse)
	if _, err := e.repo.Get(context.Background(), aws.ID); err != nil {
		t.Errorf("the subject must stay alive: %v", err)
	}

	exec(t, fmt.Sprintf(`UPDATE exams SET deleted_at = now() WHERE subject_id = '%s'`, aws.ID))
	if err := svc.Execute(context.Background(), aws.ID); err != nil {
		t.Errorf("delete once the exam is gone: %v", err)
	}
}
