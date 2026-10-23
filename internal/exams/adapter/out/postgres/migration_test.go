package postgres

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
)

func tableExists(t *testing.T, name string) bool {
	t.Helper()
	var ok bool
	if err := pool.QueryRow(context.Background(),
		`SELECT to_regclass('public.' || $1) IS NOT NULL`, name).Scan(&ok); err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestMigration0009CreatesTheExamsTable(t *testing.T) { // slice 02 §5
	skipShort(t)
	rows, err := pool.Query(context.Background(),
		`SELECT column_name FROM information_schema.columns WHERE table_name = 'exams'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		got = append(got, c)
	}
	sort.Strings(got)

	want := []string{"created_at", "deleted_at", "id", "name", "pass_percent", "question_count",
		"subject_id", "time_limit_min", "updated_at", "version"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("columns = %v, want %v", got, want)
	}
}

func TestMigration0009AddsTheAliveNameIndexAndTheHardDeleteTrigger(t *testing.T) { // slice 02 §5
	skipShort(t)
	var indexDef string
	err := pool.QueryRow(context.Background(),
		`SELECT indexdef FROM pg_indexes WHERE tablename = 'exams' AND indexdef LIKE 'CREATE UNIQUE INDEX%'`).Scan(&indexDef)
	if err != nil {
		t.Fatalf("no unique index on exams: %v", err)
	}
	for _, part := range []string{"subject_id", "lower(btrim(name))", "deleted_at IS NULL"} {
		if !strings.Contains(indexDef, part) {
			t.Errorf("unique index %q lacks %q", indexDef, part)
		}
	}

	var triggers int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM information_schema.triggers WHERE event_object_table = 'exams' AND event_manipulation = 'DELETE'`).Scan(&triggers); err != nil {
		t.Fatal(err)
	}
	if triggers != 1 {
		t.Errorf("DELETE triggers on exams = %d, want 1 (forbid_hard_delete)", triggers)
	}
}

func TestMigration0009DownDropsTheExamsTableAndUpRestoresIt(t *testing.T) { // slice 02 §5
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
	if !tableExists(t, "exams") {
		t.Fatal("exams missing after migrate up")
	}

	// Version 8 is the last studies migration; 0009 is the first exams one.
	if err := m.Migrate(8); err != nil {
		t.Fatalf("migrate down to version 8: %v", err)
	}
	if tableExists(t, "exams") {
		t.Error("exams still present after rolling back to version 8")
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	if !tableExists(t, "exams") {
		t.Error("exams missing after re-applying the migration")
	}
}
