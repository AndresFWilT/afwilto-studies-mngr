package postgres

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Spec 16 FR-0: deletion is always logical. The static half of the guarantee
// (the exams_no_delete trigger is the runtime half): no repository source file
// may contain a SQL DELETE statement.
var hardDelete = regexp.MustCompile(`(?i)\bDELETE\s+FROM\b`)

func TestExamRepositoriesNeverHardDelete(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("no Go files found: %v", err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if hardDelete.Match(src) {
			t.Errorf("%s contains a hard DELETE — set deleted_at instead (Spec 16 FR-0)", f)
		}
	}
}
