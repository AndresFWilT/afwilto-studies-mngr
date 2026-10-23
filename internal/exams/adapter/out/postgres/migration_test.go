package postgres

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/google/uuid"
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
		`SELECT indexdef FROM pg_indexes WHERE tablename = 'exams' AND indexdef LIKE 'CREATE UNIQUE INDEX%' AND indexname <> 'exams_pkey'`).Scan(&indexDef)
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

var slice03Tables = []string{"exam_classifications", "exam_categories", "questions", "question_options", "question_categories"}

func columnsOf(t *testing.T, table string) []string {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT column_name FROM information_schema.columns WHERE table_name = $1`, table)
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
	return got
}

func TestMigration0010CreatesTheQuestionTablesWithTheSpecColumns(t *testing.T) { // slice 03 §4
	skipShort(t)
	required := map[string][]string{
		"exam_classifications": {"id", "exam_id", "name", "fold_name", "position", "weight_mode", "deleted_at"},
		"exam_categories":      {"id", "classification_id", "key", "name", "fold_name", "weight_percent", "pace_questions", "pace_minutes", "deleted_at"},
		"questions": {"id", "exam_id", "key", "type", "stem", "explanation", "difficulty", "source_kind", "source_name", "origin",
			"stem_fingerprint", "version", "created_at", "updated_at", "deleted_at"},
		"question_options":    {"question_id", "position", "text", "image", "alt", "correct", "rationale", "deleted_at"},
		"question_categories": {"question_id", "category_id", "deleted_at"},
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

func TestMigration0010GuardsEveryTableAgainstHardDeletes(t *testing.T) { // slice 03 §4
	skipShort(t)
	for _, table := range slice03Tables {
		var n int
		if err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM information_schema.triggers WHERE event_object_table = $1 AND event_manipulation = 'DELETE'`, table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("DELETE triggers on %s = %d, want 1 (forbid_hard_delete)", table, n)
		}
	}
}

func TestMigration0010HardDeleteIsRejectedByTheDatabase(t *testing.T) { // FR-0 at runtime
	skipShort(t)
	e := newRepoEnv(t)
	exam := e.add(t, e.subjectID, "Trigger test")
	classID, catID, qID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	exec(t, `INSERT INTO exam_classifications (id, exam_id, name, fold_name, position) VALUES ($1, $2, 'Dominio', 'dominio', 0)`, classID, exam.ID)
	exec(t, `INSERT INTO exam_categories (id, classification_id, key, name, fold_name) VALUES ($1, $2, 'a', 'A', 'a')`, catID, classID)
	exec(t, `INSERT INTO questions (id, exam_id, key, type, stem, source_kind, source_name, stem_fingerprint) VALUES ($1, $2, 'q', 'single', 's', 'own', 'x', 'f')`, qID, exam.ID)
	exec(t, `INSERT INTO question_options (question_id, position, text, correct) VALUES ($1, 0, 'a', true)`, qID)
	exec(t, `INSERT INTO question_categories (question_id, category_id) VALUES ($1, $2)`, qID, catID)

	for _, table := range slice03Tables {
		if _, err := pool.Exec(context.Background(), "DELETE FROM "+table); err == nil {
			t.Errorf("hard DELETE on %s succeeded", table)
		}
	}
}

func TestMigration0010AliveUniquenessIndexes(t *testing.T) { // slice 03 §4
	skipShort(t)
	e := newRepoEnv(t)
	exam := e.add(t, e.subjectID, "Index test")
	classID := uuid.NewString()
	exec(t, `INSERT INTO exam_classifications (id, exam_id, name, fold_name, position) VALUES ($1, $2, 'Dominio', 'dominio', 0)`, classID, exam.ID)
	exec(t, `INSERT INTO exam_categories (id, classification_id, key, name, fold_name) VALUES ($1, $2, 'a', 'A', 'a')`, uuid.NewString(), classID)
	exec(t, `INSERT INTO questions (id, exam_id, key, type, stem, source_kind, source_name, stem_fingerprint) VALUES ($1, $2, 'q', 'single', 's', 'own', 'x', 'f')`, uuid.NewString(), exam.ID)

	rejected := map[string]string{
		"classification fold name per exam":     `INSERT INTO exam_classifications (id, exam_id, name, fold_name, position) VALUES ('` + uuid.NewString() + `', '` + exam.ID + `', 'DOMINIO', 'dominio', 1)`,
		"a second primary classification":       `INSERT INTO exam_classifications (id, exam_id, name, fold_name, position) VALUES ('` + uuid.NewString() + `', '` + exam.ID + `', 'Otro', 'otro', 0)`,
		"category fold name per classification": `INSERT INTO exam_categories (id, classification_id, key, name, fold_name) VALUES ('` + uuid.NewString() + `', '` + classID + `', 'a2', 'a', 'a')`,
		"question key per exam":                 `INSERT INTO questions (id, exam_id, key, type, stem, source_kind, source_name, stem_fingerprint) VALUES ('` + uuid.NewString() + `', '` + exam.ID + `', 'q', 'single', 's', 'own', 'x', 'f')`,
		"bad question key":                      `INSERT INTO questions (id, exam_id, key, type, stem, source_kind, source_name, stem_fingerprint) VALUES ('` + uuid.NewString() + `', '` + exam.ID + `', 'Bad Key', 'single', 's', 'own', 'x', 'f')`,
		"bad question type":                     `INSERT INTO questions (id, exam_id, key, type, stem, source_kind, source_name, stem_fingerprint) VALUES ('` + uuid.NewString() + `', '` + exam.ID + `', 'q2', 'ordering', 's', 'own', 'x', 'f')`,
		"pace without minutes":                  `INSERT INTO exam_categories (id, classification_id, key, name, fold_name, pace_questions) VALUES ('` + uuid.NewString() + `', '` + classID + `', 'p', 'P', 'p', 5)`,
		"pace of zero":                          `INSERT INTO exam_categories (id, classification_id, key, name, fold_name, pace_questions, pace_minutes) VALUES ('` + uuid.NewString() + `', '` + classID + `', 'p', 'P', 'p', 0, 5)`,
		"classification position 2":             `INSERT INTO exam_classifications (id, exam_id, name, fold_name, position) VALUES ('` + uuid.NewString() + `', '` + exam.ID + `', 'N', 'n', 2)`,
	}
	for name, stmt := range rejected {
		if _, err := pool.Exec(context.Background(), stmt); err == nil {
			t.Errorf("%s: the insert was accepted", name)
		}
	}

	// after logical deletion the names are free again
	exec(t, `UPDATE exam_classifications SET deleted_at = now() WHERE exam_id = $1`, exam.ID)
	exec(t, `UPDATE questions SET deleted_at = now() WHERE exam_id = $1`, exam.ID)
	exec(t, `INSERT INTO exam_classifications (id, exam_id, name, fold_name, position) VALUES ($1, $2, 'Dominio', 'dominio', 0)`, uuid.NewString(), exam.ID)
	exec(t, `INSERT INTO questions (id, exam_id, key, type, stem, source_kind, source_name, stem_fingerprint) VALUES ($1, $2, 'q', 'single', 's', 'own', 'x', 'f')`, uuid.NewString(), exam.ID)
}

func TestMigration0010DownDropsTheTablesAndUpRestoresThem(t *testing.T) { // slice 03 §4
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
	if err := m.Migrate(9); err != nil {
		t.Fatalf("migrate down to version 9: %v", err)
	}
	for _, table := range slice03Tables {
		if tableExists(t, table) {
			t.Errorf("%s still present after rolling back to version 9", table)
		}
	}
	if !tableExists(t, "exams") {
		t.Error("exams must survive the rollback of 0010")
	}
	if err := m.Up(); err != nil {
		t.Fatalf("migrate up again: %v", err)
	}
	for _, table := range slice03Tables {
		if !tableExists(t, table) {
			t.Errorf("%s missing after re-applying", table)
		}
	}
}
