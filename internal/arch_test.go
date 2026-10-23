package internal

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const modulePath = "github.com/afwilto/studies-mngr"

// Spec 18 D-10 made executable: the two bounded contexts never import each
// other; both may import internal/platform/**.

// importsUnder lists "file imports pkg" for every import of a package whose
// path starts with forbiddenPrefix, in the .go files (tests included) under
// root. It reports an error when root holds no Go file, so an empty or
// missing tree can never pass vacuously.
func importsUnder(t *testing.T, root, forbiddenPrefix string) (offenders []string, goFiles int) {
	t.Helper()
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		goFiles++
		for _, imp := range f.Imports {
			pkg := strings.Trim(imp.Path.Value, `"`)
			if pkg == forbiddenPrefix || strings.HasPrefix(pkg, forbiddenPrefix+"/") {
				rel, _ := filepath.Rel(root, path)
				offenders = append(offenders, rel+" imports "+pkg)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return offenders, goFiles
}

func TestExamsNeverImportStudies(t *testing.T) { // S01-3
	offenders, files := importsUnder(t, "exams", modulePath+"/internal/studies")
	if files == 0 {
		t.Fatal("internal/exams holds no Go files: the skeleton is missing")
	}
	for _, o := range offenders {
		t.Errorf("internal/exams/%s — contexts must not import each other (D-10)", o)
	}
}

func TestStudiesNeverImportExams(t *testing.T) { // S01-3
	offenders, files := importsUnder(t, "studies", modulePath+"/internal/exams")
	if files == 0 {
		t.Fatal("internal/studies holds no Go files")
	}
	for _, o := range offenders {
		t.Errorf("internal/studies/%s — contexts must not import each other (D-10)", o)
	}
}

func TestIsolationDetector(t *testing.T) { // S01-3 mutation check, kept executable
	root := t.TempDir()
	write := func(rel, src string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("ok.go", "package a\nimport _ \""+modulePath+"/internal/platform/httpx\"\n")
	write("sibling.go", "package a\nimport _ \""+modulePath+"/internal/exams/domain\"\n")
	write("deep/leak.go", "package b\nimport _ \""+modulePath+"/internal/studies/app/usecase\"\n")
	write("deep/leak_test.go", "package b\nimport _ \""+modulePath+"/internal/studies\"\n")
	write("lookalike.go", "package a\nimport _ \""+modulePath+"/internal/studiesfoo\"\n")

	got, files := importsUnder(t, root, modulePath+"/internal/studies")

	want := map[string]bool{
		filepath.Join("deep", "leak.go") + " imports " + modulePath + "/internal/studies/app/usecase": true,
		filepath.Join("deep", "leak_test.go") + " imports " + modulePath + "/internal/studies":        true,
	}
	if files != 5 {
		t.Errorf("scanned %d files, want 5", files)
	}
	if len(got) != len(want) {
		t.Fatalf("offenders = %v, want exactly %v", got, want)
	}
	for _, o := range got {
		if !want[o] {
			t.Errorf("unexpected offender %q", o)
		}
	}
}

func TestExamsSkeletonPackagesExist(t *testing.T) { // slice 01 §1.5
	want := map[string]string{
		"exams/domain":               "domain",
		"exams/app/port/in":          "in",
		"exams/app/port/out":         "out",
		"exams/app/usecase":          "usecase",
		"exams/adapter/in/http":      "http",
		"exams/adapter/out/postgres": "postgres",
	}
	fset := token.NewFileSet()
	for dir, pkg := range want {
		f, err := parser.ParseFile(fset, filepath.Join(dir, "doc.go"), nil, parser.ParseComments)
		if err != nil {
			t.Errorf("%s/doc.go: %v", dir, err)
			continue
		}
		if f.Name.Name != pkg {
			t.Errorf("%s/doc.go declares package %q, want %q", dir, f.Name.Name, pkg)
		}
		if f.Doc == nil || !strings.HasPrefix(f.Doc.Text(), "Package "+pkg+" ") {
			t.Errorf("%s/doc.go needs a package comment starting %q", dir, "Package "+pkg+" ")
		}
	}
}
