package postgres

import (
	"testing"

	"github.com/google/uuid"
)

func TestSubjectDirectoryIsAliveOnlyForAliveSubjects(t *testing.T) { // S02-4
	e := newRepoEnv(t)
	dir := NewSubjectDirectory(pool)
	deleted := e.otherSubject(t, "Deleted")
	exec(t, `UPDATE subjects SET deleted_at = now() WHERE id = $1`, deleted)

	for name, tc := range map[string]struct {
		id   string
		want bool
	}{
		"alive":   {e.subjectID, true},
		"deleted": {deleted, false},
		"unknown": {uuid.NewString(), false},
	} {
		got, err := dir.IsAlive(ctx, tc.id)
		if err != nil || got != tc.want {
			t.Errorf("%s: IsAlive = (%v, %v), want %v", name, got, err, tc.want)
		}
	}
}
