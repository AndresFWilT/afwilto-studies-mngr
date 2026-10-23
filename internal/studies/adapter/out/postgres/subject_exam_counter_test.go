package postgres

import (
	"context"
	"fmt"
	"testing"
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
