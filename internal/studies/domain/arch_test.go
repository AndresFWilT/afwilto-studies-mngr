package domain

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The manifesto's grep rule made executable: nothing under the domain tree may
// import technology vocabulary — only a small stdlib allowlist. Test files get
// a few extra stdlib packages, but only test files.
var (
	domainAllowed = map[string]bool{
		"errors": true, "fmt": true, "strings": true, "time": true,
		"unicode": true, "regexp": true, "slices": true,
	}
	testOnlyAllowed = map[string]bool{
		"testing": true, "go/parser": true, "go/token": true, "io/fs": true,
		"os": true, "path/filepath": true,
	}
)

func findImpureImports(t *testing.T, root string) []string {
	t.Helper()
	fset := token.NewFileSet()
	var offenders []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return err
		}
		isTest := strings.HasSuffix(path, "_test.go")
		for _, imp := range f.Imports {
			pkg := strings.Trim(imp.Path.Value, `"`)
			if domainAllowed[pkg] || (isTest && testOnlyAllowed[pkg]) {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			offenders = append(offenders, rel+" imports "+pkg)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return offenders
}

func TestDomainImportsArePure(t *testing.T) {
	for _, o := range findImpureImports(t, ".") {
		t.Errorf("%s — the domain must stay technology-free", o)
	}
}

func TestImpureImportDetector(t *testing.T) {
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
	write("ok.go", "package d\nimport \"strings\"\nvar _ = strings.TrimSpace\n")
	write("ok_test.go", "package d\nimport _ \"os\"\n")
	write("leak.go", "package d\nimport _ \"os\"\n")
	write("sub/c.go", "package sub\nimport _ \"net/http\"\n")

	got := findImpureImports(t, root)

	want := map[string]bool{"leak.go imports os": true, filepath.Join("sub", "c.go") + " imports net/http": true}
	if len(got) != len(want) {
		t.Fatalf("offenders = %v, want exactly %v", got, want)
	}
	for _, o := range got {
		if !want[o] {
			t.Errorf("unexpected offender %q", o)
		}
	}
}
