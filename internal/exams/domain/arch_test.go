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

// The manifesto's grep rule made executable for the exams domain (S01-4):
// nothing under the domain tree may import technology vocabulary, only this
// allowlist. Test files get a few extra stdlib packages, but only test files.
var (
	domainAllowed = map[string]bool{
		// the studies stdlib allowlist
		"errors": true, "fmt": true, "strings": true, "time": true,
		"unicode": true, "regexp": true, "slices": true,
		// slice 01 additions: draw, hashing, sorting
		"math": true, "math/rand/v2": true, "crypto/sha256": true,
		"encoding/hex": true, "sort": true, "unicode/utf8": true,
		// accent folding (slice 03)
		"golang.org/x/text/unicode/norm": true,
		"golang.org/x/text/runes":        true,
		"golang.org/x/text/transform":    true,
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

func TestDomainImportsArePure(t *testing.T) { // S01-4
	for _, o := range findImpureImports(t, ".") {
		t.Errorf("%s — the domain must stay technology-free", o)
	}
}

func TestImpureImportDetector(t *testing.T) { // S01-4 mutation check, kept executable
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
	write("ok.go", "package d\nimport (\n\t_ \"strings\"\n\t_ \"math\"\n\t_ \"math/rand/v2\"\n\t_ \"crypto/sha256\"\n\t_ \"encoding/hex\"\n\t_ \"sort\"\n\t_ \"unicode/utf8\"\n"+
		"\t_ \"golang.org/x/text/unicode/norm\"\n\t_ \"golang.org/x/text/runes\"\n\t_ \"golang.org/x/text/transform\"\n)\n")
	write("ok_test.go", "package d\nimport _ \"os\"\n")
	write("leak.go", "package d\nimport _ \"os\"\n")
	write("randv1.go", "package d\nimport _ \"math/rand\"\n")
	write("otherx.go", "package d\nimport _ \"golang.org/x/text/language\"\n")
	write("sub/c.go", "package sub\nimport _ \"net/http\"\n")
	write("sub/studies.go", "package sub\nimport _ \"github.com/afwilto/studies-mngr/internal/studies/domain\"\n")

	got := findImpureImports(t, root)

	want := map[string]bool{
		"leak.go imports os":                               true,
		"randv1.go imports math/rand":                      true,
		"otherx.go imports golang.org/x/text/language":     true,
		filepath.Join("sub", "c.go") + " imports net/http": true,
		filepath.Join("sub", "studies.go") + " imports github.com/afwilto/studies-mngr/internal/studies/domain": true,
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
