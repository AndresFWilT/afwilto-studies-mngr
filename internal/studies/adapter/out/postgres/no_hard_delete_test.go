package postgres

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Spec 16 FR-0: deletion is always logical. This is the static half of the
// guarantee (the DB trigger is the runtime half): no repository may even
// contain a SQL DELETE statement.
var hardDeletePattern = regexp.MustCompile(`(?i)\bDELETE\s+FROM\b`)

func findHardDeletes(t *testing.T, dir string) []string {
	t.Helper()
	var offenders []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if hardDeletePattern.Match(src) {
			rel, _ := filepath.Rel(dir, path)
			offenders = append(offenders, rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return offenders
}

func TestRepositoriesNeverHardDelete(t *testing.T) {
	if offenders := findHardDeletes(t, "."); len(offenders) > 0 {
		t.Errorf("hard DELETE found in %v — set deleted_at instead (Spec 16 FR-0)", offenders)
	}
}

func TestHardDeleteDetectorCatchesViolations(t *testing.T) {
	cases := map[string]string{
		"single line": "package x\nconst q = `DELETE FROM notes WHERE id = $1`\n",
		"lower case":  "package x\nconst q = \"delete from notes\"\n",
		"multi line":  "package x\nconst q = `\n\tDELETE\n\tFROM notes`\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "repo.go"), []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := findHardDeletes(t, dir); len(got) != 1 {
				t.Errorf("offenders = %v, want [repo.go]", got)
			}
		})
	}
}

func TestHardDeleteDetectorScansSubdirectories(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "notes")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "r.go"), []byte("package notes\nconst q = \"DELETE FROM notes\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := findHardDeletes(t, dir); len(got) != 1 || got[0] != filepath.Join("notes", "r.go") {
		t.Errorf("offenders = %v, want [notes/r.go]", got)
	}
}

func TestHardDeleteDetectorIgnoresSoftDeleteAndTests(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"repo.go":      "package x\nconst q = `UPDATE notes SET deleted_at = now() WHERE id = $1`\n",
		"repo_test.go": "package x\nconst q = `DELETE FROM notes`\n",
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got := findHardDeletes(t, dir); len(got) != 0 {
		t.Errorf("offenders = %v, want none", got)
	}
}
