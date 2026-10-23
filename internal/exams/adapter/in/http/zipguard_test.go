package http

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// zent is one entry of a generated test zip.
type zent struct {
	name  string
	data  []byte
	mode  fs.FileMode // 0 = a regular file
	store bool        // Store instead of Deflate
}

func buildZip(t testing.TB, entries ...zent) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate, Modified: time.Date(2026, 10, 22, 9, 0, 0, 0, time.UTC)}
		if e.store {
			h.Method = zip.Store
		}
		if e.mode != 0 {
			h.SetMode(e.mode)
		}
		f, err := w.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(e.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// patchDeclaredSize rewrites the uncompressed size that the central directory
// declares for an entry, which is what archive/zip and every guard sees.
func patchDeclaredSize(t testing.TB, data []byte, name string, size uint32) []byte {
	t.Helper()
	out := slices.Clone(data)
	sig := []byte("PK\x01\x02")
	for i := 0; i+46 <= len(out); {
		j := bytes.Index(out[i:], sig)
		if j < 0 {
			break
		}
		i += j
		nameLen := int(binary.LittleEndian.Uint16(out[i+28:]))
		if i+46+nameLen <= len(out) && string(out[i+46:i+46+nameLen]) == name {
			binary.LittleEndian.PutUint32(out[i+24:], size)
			return out
		}
		i += 4
	}
	t.Fatalf("no central directory entry %q", name)
	return nil
}

const rootJSON = `{"format":"afwilto-questions","formatVersion":1,"questions":[]}`

func questionsEntry() zent { return zent{name: "questions.json", data: []byte(rootJSON)} }

func zipRule(t *testing.T, err error) string {
	t.Helper()
	var v *domain.ValidationError
	if !errors.As(err, &v) || len(v.Violations) != 1 {
		t.Fatalf("err = %v, want one validation violation", err)
	}
	if v.Violations[0].Field != "file" || v.Violations[0].Message == "" {
		t.Errorf("violation = %+v, want field file and a message", v.Violations[0])
	}
	return v.Violations[0].Rule
}

func wantRule(t *testing.T, zipBytes []byte, limits zipLimits, rule string) {
	t.Helper()
	z, err := openImportZip(zipBytes, limits)
	if err == nil {
		t.Fatalf("the zip was accepted (%v), want rule %s", z, rule)
	}
	if got := zipRule(t, err); got != rule {
		t.Errorf("rule = %s (%v), want %s", got, err, rule)
	}
}

func filler(n int) []zent {
	var out []zent
	for i := 0; i < n; i++ {
		out = append(out, zent{name: fmt.Sprintf("img/%05d.png", i), data: []byte{byte(i)}, store: true})
	}
	return out
}

func TestAZipOfExactlyThousandEntriesIsAcceptedAndOneMoreIsZipEntries(t *testing.T) { // S05-2 1,001 entries, boundary
	ok := buildZip(t, append([]zent{questionsEntry()}, filler(999)...)...)
	tooMany := buildZip(t, append([]zent{questionsEntry()}, filler(1000)...)...)

	if _, err := openImportZip(ok, defaultZipLimits()); err != nil {
		t.Errorf("1,000 entries rejected: %v", err)
	}
	wantRule(t, tooMany, defaultZipLimits(), domain.RuleZipEntries)
}

func TestAZipWithMoreEntriesThanTheDirectoryCountFieldHoldsIsZipEntries(t *testing.T) { // the count is read from the end record, before the directory is parsed
	var many []zent
	for i := 0; i < 70000; i++ {
		many = append(many, zent{name: fmt.Sprintf("e%d", i), store: true})
	}

	wantRule(t, buildZip(t, many...), defaultZipLimits(), domain.RuleZipEntries)
}

func TestAnEntryThatDeclaresMoreThanTheTotalLimitIsZipSize(t *testing.T) { // S05-2 an entry declaring 200 MiB
	data := patchDeclaredSize(t, buildZip(t, questionsEntry(), zent{name: "img/big.png", data: []byte("x")}), "img/big.png", 200<<20)

	wantRule(t, data, defaultZipLimits(), domain.RuleZipSize)
}

func TestDeclaredSizesAreSummedAndTheLimitItselfIsAllowed(t *testing.T) { // FR-6 100 MiB total, boundary
	limits := zipLimits{MaxEntries: 10, MaxTotal: 100, MaxRootJSON: 1000}
	build := func(a, b uint32) []byte {
		z := buildZip(t, zent{name: "questions.json", data: []byte(rootJSON)}, zent{name: "a.png", data: []byte("x")}, zent{name: "b.png", data: []byte("y")})
		z = patchDeclaredSize(t, z, "questions.json", 0)
		return patchDeclaredSize(t, patchDeclaredSize(t, z, "a.png", a), "b.png", b)
	}

	if _, err := openImportZip(build(60, 40), limits); err != nil {
		t.Errorf("a total of exactly the limit was rejected: %v", err)
	}
	wantRule(t, build(60, 41), limits, domain.RuleZipSize)
	wantRule(t, build(0xFFFFFFFE, 0xFFFFFFFE), limits, domain.RuleZipSize)
}

func TestEntryNamesThatEscapeAreZipPath(t *testing.T) { // S05-2 ../x.png and /abs.png, AC-12
	for _, name := range []string{
		"../x.png", "img/../../x.png", "a/../b.png", "..", "/abs.png", "//abs.png", `img\a.png`, `..\x.png`, `C:\x.png`, "C:/x.png", "c:x.png", "img/a\x00.png",
	} {
		t.Run(name, func(t *testing.T) {
			wantRule(t, buildZip(t, questionsEntry(), zent{name: name, data: []byte("x")}), defaultZipLimits(), domain.RuleZipPath)
		})
	}
}

func TestOrdinaryNamesThatMerelyContainDotsAreAccepted(t *testing.T) { // the check is per segment
	data := buildZip(t, questionsEntry(), zent{name: "img/", mode: fs.ModeDir | 0o755},
		zent{name: "img/..hidden.png", data: []byte("x")}, zent{name: "img/a..b.png", data: []byte("y")},
		zent{name: "img/.dot/c.png", data: []byte("z")}, zent{name: "__MACOSX/._questions.json", data: []byte("m")})

	if _, err := openImportZip(data, defaultZipLimits()); err != nil {
		t.Errorf("rejected: %v", err)
	}
}

func TestASymlinkEntryIsZipPath(t *testing.T) { // S05-2 a symlink entry
	data := buildZip(t, questionsEntry(), zent{name: "img/link.png", data: []byte("/etc/passwd"), mode: fs.ModeSymlink | 0o777})

	wantRule(t, data, defaultZipLimits(), domain.RuleZipPath)
}

func TestTwoEntriesWithTheSameNameAreZipPath(t *testing.T) { // ambiguous: which one would be sniffed and which stored?
	wantRule(t, buildZip(t, questionsEntry(), zent{name: "a.png", data: []byte("1")}, zent{name: "a.png", data: []byte("2")}), defaultZipLimits(), domain.RuleZipPath)
}

func TestExactlyOneJSONAtTheRootIsRequired(t *testing.T) { // S05-2 two root JSON files, FR-6
	two := buildZip(t, questionsEntry(), zent{name: "more.json", data: []byte("{}")})
	none := buildZip(t, zent{name: "img/a.png", data: []byte("x")})
	nested := buildZip(t, zent{name: "folder/questions.json", data: []byte(rootJSON)})

	wantRule(t, two, defaultZipLimits(), domain.RuleZipLayout)
	wantRule(t, none, defaultZipLimits(), domain.RuleZipLayout)
	wantRule(t, nested, defaultZipLimits(), domain.RuleZipLayout)
}

func TestAJSONInAFolderAndAnUppercaseExtensionAreHandledByTheRootRule(t *testing.T) {
	z, err := openImportZip(buildZip(t, zent{name: "QUESTIONS.JSON", data: []byte(rootJSON)}, zent{name: "docs/other.json", data: []byte("{}")}), defaultZipLimits())
	if err != nil {
		t.Fatal(err)
	}

	got, err := z.RootJSON()
	if err != nil || string(got) != rootJSON {
		t.Errorf("RootJSON = %q, %v", got, err)
	}
	if want := []string{"docs/other.json"}; !slices.Equal(z.Names(), want) {
		t.Errorf("Names = %v, want %v: a nested JSON is an ordinary entry", z.Names(), want)
	}
}

func TestNamesListsFileEntriesSortedWithoutDirectoriesAndTheRootJSON(t *testing.T) {
	z, err := openImportZip(buildZip(t, zent{name: "img/b.png", data: []byte("b")}, questionsEntry(), zent{name: "img/", mode: fs.ModeDir | 0o755},
		zent{name: "img/a.png", data: []byte("a")}, zent{name: "readme.txt", data: []byte("r")}), defaultZipLimits())
	if err != nil {
		t.Fatal(err)
	}

	if want := []string{"img/a.png", "img/b.png", "readme.txt"}; !slices.Equal(z.Names(), want) {
		t.Errorf("Names = %v, want %v", z.Names(), want)
	}
}

func TestOpeningNeverDecompressesAnEntry(t *testing.T) { // "checked BEFORE any entry is decompressed"
	good := buildZip(t, questionsEntry(), zent{name: "img/a.png", data: bytes.Repeat([]byte("abcdefgh"), 500)})
	corrupt := corruptBody(t, good, "img/a.png")

	z, err := openImportZip(corrupt, defaultZipLimits())
	if err != nil {
		t.Fatalf("a zip with a corrupt entry body was rejected at open: %v", err)
	}
	if _, _, err := z.ReadEntry("img/a.png", 1<<20); err == nil {
		t.Error("reading the corrupt entry succeeded")
	}
}

// corruptBody overwrites the compressed bytes of an entry with 0xFF.
func corruptBody(t testing.TB, data []byte, name string) []byte {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			off, err := f.DataOffset()
			if err != nil {
				t.Fatal(err)
			}
			out := slices.Clone(data)
			for i := int64(0); i < int64(f.CompressedSize64); i++ {
				out[off+i] = 0xFF
			}
			return out
		}
	}
	t.Fatalf("no entry %q", name)
	return nil
}

func TestAnEarlierRuleWinsWithoutTouchingTheBodies(t *testing.T) { // checks run on names and headers only
	good := buildZip(t, questionsEntry(), zent{name: "../evil.png", data: bytes.Repeat([]byte("z"), 4000)})

	wantRule(t, corruptBody(t, good, "../evil.png"), defaultZipLimits(), domain.RuleZipPath)
}

func TestEntryCountIsCheckedBeforeSizesPathsAndLayout(t *testing.T) { // order: entries, size, path, layout
	entries := []zent{{name: "../a", data: []byte("x")}, {name: "b"}, {name: "c"}}
	limits := zipLimits{MaxEntries: 2, MaxTotal: 0, MaxRootJSON: 10}

	wantRule(t, buildZip(t, entries...), limits, domain.RuleZipEntries)
	wantRule(t, buildZip(t, entries[:2]...), limits, domain.RuleZipSize)
	wantRule(t, buildZip(t, zent{name: "../a"}, zent{name: "b"}), zipLimits{MaxEntries: 2, MaxTotal: 10, MaxRootJSON: 10}, domain.RuleZipPath)
	wantRule(t, buildZip(t, zent{name: "a"}, zent{name: "b"}), zipLimits{MaxEntries: 2, MaxTotal: 10, MaxRootJSON: 10}, domain.RuleZipLayout)
}

func TestAnEntryThatLiesAboutItsSizeIsZipSizeWhenRead(t *testing.T) { // declared AND actual, counting reader
	data := patchDeclaredSize(t, buildZip(t, zent{name: "questions.json", data: bytes.Repeat([]byte(" "), 5000)}), "questions.json", 100)
	z, err := openImportZip(data, defaultZipLimits())
	if err != nil {
		t.Fatal(err)
	}

	_, err = z.RootJSON()

	if err == nil || zipRule(t, err) != domain.RuleZipSize {
		t.Errorf("err = %v, want zip_size", err)
	}
}

func TestAnImageEntryThatLiesAboutItsSizeIsZipSizeWhenRead(t *testing.T) { // the counting reader on every entry, not only the JSON
	data := patchDeclaredSize(t, buildZip(t, questionsEntry(), zent{name: "img/a.png", data: bytes.Repeat([]byte("q"), 5000)}), "img/a.png", 10)
	z, err := openImportZip(data, defaultZipLimits())
	if err != nil {
		t.Fatal(err)
	}

	_, _, err = z.ReadEntry("img/a.png", 1<<20)

	if err == nil || zipRule(t, err) != domain.RuleZipSize {
		t.Errorf("err = %v, want zip_size", err)
	}
}

func TestARootJSONAboveItsLimitIsZipSize(t *testing.T) { // the .json cap also applies inside a zip
	limits := zipLimits{MaxEntries: 10, MaxTotal: 1 << 20, MaxRootJSON: 100}
	z, err := openImportZip(buildZip(t, zent{name: "questions.json", data: []byte(`{"format":"` + strings.Repeat("x", 200) + `"}`)}), limits)
	if err != nil {
		t.Fatal(err)
	}

	_, err = z.RootJSON()

	if err == nil || zipRule(t, err) != domain.RuleZipSize {
		t.Errorf("err = %v, want zip_size", err)
	}
}

func TestReadEntryReportsAnEntryOverTheLimitWithoutReadingIt(t *testing.T) { // media_size is decided from the header
	good := buildZip(t, questionsEntry(), zent{name: "img/a.png", data: bytes.Repeat([]byte("abcdefgh"), 500)})
	z, err := openImportZip(corruptBody(t, good, "img/a.png"), defaultZipLimits())
	if err != nil {
		t.Fatal(err)
	}

	data, exceeded, err := z.ReadEntry("img/a.png", 1000)

	if err != nil || !exceeded || data != nil {
		t.Errorf("ReadEntry = %d bytes, exceeded %v, err %v; want exceeded without reading", len(data), exceeded, err)
	}
}

func TestReadEntryReturnsTheBytesOfAnEntryWithinTheLimit(t *testing.T) {
	z, err := openImportZip(buildZip(t, questionsEntry(), zent{name: "img/a.png", data: []byte("hello")}), defaultZipLimits())
	if err != nil {
		t.Fatal(err)
	}

	data, exceeded, err := z.ReadEntry("img/a.png", 5) // the limit itself is allowed

	if err != nil || exceeded || string(data) != "hello" {
		t.Errorf("ReadEntry = %q, exceeded %v, err %v", data, exceeded, err)
	}
	if _, _, err := z.ReadEntry("img/none.png", 5); err == nil {
		t.Error("reading an entry that is not in the zip succeeded")
	}
}

func TestBytesThatAreNotAZipAreFileType(t *testing.T) { // PK magic but no end record
	for name, data := range map[string][]byte{
		"magic only":    []byte("PK\x03\x04"),
		"junk":          append([]byte("PK\x03\x04"), bytes.Repeat([]byte{7}, 200)...),
		"truncated zip": buildZip(t, questionsEntry())[:40],
	} {
		z, err := openImportZip(data, defaultZipLimits())
		if err == nil {
			t.Errorf("%s: accepted (%v)", name, z)
			continue
		}
		if got := zipRule(t, err); got != domain.RuleFileType {
			t.Errorf("%s: rule = %s, want file_type", name, got)
		}
	}
}

func TestEndRecordCountReadsTheEntryCountWithoutParsingTheDirectory(t *testing.T) { // memory guard before archive/zip builds its entry list
	three := buildZip(t, questionsEntry(), zent{name: "a"}, zent{name: "b"})
	var many []zent
	for i := 0; i < 70000; i++ {
		many = append(many, zent{name: fmt.Sprintf("e%d", i), store: true})
	}

	if n, ok := endRecordCount(three); !ok || n != 3 {
		t.Errorf("endRecordCount(3 entries) = %d, %v", n, ok)
	}
	if n, ok := endRecordCount(buildZip(t, many...)); !ok || n != 0xFFFF {
		t.Errorf("endRecordCount(70,000 entries) = %d, %v; want the zip64 marker 65535", n, ok)
	}
	if _, ok := endRecordCount([]byte("PK\x03\x04 and no end record")); ok {
		t.Error("an end record was found in junk")
	}
}

// ── phase-2 fix 6: the entry count cannot be bypassed through the 16-bit field ──

// craftedZip is a zip made of central-directory headers only, which is all
// the guard and archive/zip's NewReader read. recorded is the count the end
// record claims; the directory really holds n headers.
func craftedZip(n int, recorded uint16, nameLen, extraLen, commentLen int) []byte {
	var buf bytes.Buffer
	for i := 0; i < n; i++ {
		h := make([]byte, 46)
		binary.LittleEndian.PutUint32(h[0:], 0x02014b50)
		binary.LittleEndian.PutUint16(h[28:], uint16(nameLen))
		binary.LittleEndian.PutUint16(h[30:], uint16(extraLen))
		binary.LittleEndian.PutUint16(h[32:], uint16(commentLen))
		buf.Write(h)
		name := fmt.Sprintf("%0*d", nameLen, i)
		buf.WriteString(name[len(name)-nameLen:])
		buf.Write(make([]byte, extraLen))
		buf.WriteString(strings.Repeat("c", commentLen))
	}
	cdSize := buf.Len()
	eocd := make([]byte, 22)
	binary.LittleEndian.PutUint32(eocd[0:], 0x06054b50)
	binary.LittleEndian.PutUint16(eocd[8:], recorded)
	binary.LittleEndian.PutUint16(eocd[10:], recorded)
	binary.LittleEndian.PutUint32(eocd[12:], uint32(cdSize))
	binary.LittleEndian.PutUint32(eocd[16:], 0)
	buf.Write(eocd)
	return buf.Bytes()
}

func allocatedBy(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestAnEndRecordCountThatWrapsAroundCannotHideTensOfThousandsOfEntries(t *testing.T) { // fix 6
	data := craftedZip(65536+500, 500, 6, 0, 0) // the record says 500; the directory holds 66,036 empty entries

	var err error
	alloc := allocatedBy(func() { _, err = openImportZip(data, defaultZipLimits()) })

	if err == nil || zipRule(t, err) != domain.RuleZipEntries {
		t.Fatalf("err = %v, want zip_entries", err)
	}
	if alloc > 1<<20 {
		t.Errorf("rejecting it allocated %d bytes: archive/zip parsed the directory (budget 1 MiB)", alloc)
	}
}

func TestACentralDirectoryOverOneMebibyteIsZipEntriesWithoutBeingWalked(t *testing.T) { // fix 6
	data := craftedZip(1000, 1000, 1040, 0, 0) // 1,000 entries of 1,086 bytes: 1,086,000 > 1,048,576

	if err := guardCentralDirectory(data, 1000); err == nil || zipRule(t, err) != domain.RuleZipEntries {
		t.Errorf("err = %v, want zip_entries for a directory over 1 MiB", err)
	}
}

func TestACentralDirectoryUnderTheCapWithTheRealCountAboveTheLimitIsZipEntries(t *testing.T) { // fix 6: the walk, not the record
	cases := map[string][3]int{ // name, extra, comment lengths: the walk must step over all three
		"names only":             {10, 0, 0},
		"names and extra fields": {10, 12, 0},
		"names and comments":     {10, 0, 30},
		"all three":              {7, 5, 9},
	}
	for name, lens := range cases {
		data := craftedZip(1500, 500, lens[0], lens[1], lens[2]) // the record says 500: it passes the count check

		err := guardCentralDirectory(data, 1000)

		if err == nil || zipRule(t, err) != domain.RuleZipEntries {
			t.Errorf("%s: err = %v, want zip_entries (1,500 real headers)", name, err)
		}
	}
}

func TestADirectoryWithExactlyTheLimitOfEntriesPassesTheGuard(t *testing.T) { // fix 6 boundary
	for name, data := range map[string][]byte{
		"1,000 short names":                  craftedZip(1000, 1000, 10, 0, 0),
		"1,000 entries with every field set": craftedZip(1000, 1000, 200, 20, 40),
		"empty archive":                      craftedZip(0, 0, 0, 0, 0),
	} {
		if err := guardCentralDirectory(data, 1000); err != nil {
			t.Errorf("%s: %v, want accepted", name, err)
		}
	}
}

func TestTheGuardLeavesRealZipsAndDamagedOnesToArchiveZip(t *testing.T) { // fix 6: no false rejections
	real := buildZip(t, questionsEntry(), zent{name: "img/a.png", data: []byte("x")})

	for name, data := range map[string][]byte{
		"a real zip":          real,
		"junk with PK magic":  []byte("PK\x03\x04 and no end record"),
		"a truncated zip":     real[:len(real)-10],
		"a zip with a prefix": append([]byte("prefix-bytes-"), real...),
	} {
		if err := guardCentralDirectory(data, 1000); err != nil {
			t.Errorf("%s: guard answered %v; damaged input is archive/zip's to report", name, err)
		}
	}
}

// craftedZip64 hides the real directory behind zip64 records: the classic end
// record claims 500 entries and a tiny directory at the marker offset
// 0xFFFFFFFF, so archive/zip reads the 64-bit values instead.
func craftedZip64(n int) []byte {
	classic := craftedZip(n, 500, 6, 0, 0)
	cd := classic[:len(classic)-22]
	var buf bytes.Buffer
	buf.Write(cd)
	zip64Pos := buf.Len()
	rec := make([]byte, 56)
	binary.LittleEndian.PutUint32(rec[0:], 0x06064b50)
	binary.LittleEndian.PutUint64(rec[4:], 44)
	binary.LittleEndian.PutUint64(rec[24:], uint64(n))
	binary.LittleEndian.PutUint64(rec[32:], uint64(n))
	binary.LittleEndian.PutUint64(rec[40:], uint64(len(cd)))
	binary.LittleEndian.PutUint64(rec[48:], 0)
	buf.Write(rec)
	loc := make([]byte, 20)
	binary.LittleEndian.PutUint32(loc[0:], 0x07064b50)
	binary.LittleEndian.PutUint64(loc[8:], uint64(zip64Pos))
	binary.LittleEndian.PutUint32(loc[16:], 1)
	buf.Write(loc)
	eocd := make([]byte, 22)
	binary.LittleEndian.PutUint32(eocd[0:], 0x06054b50)
	binary.LittleEndian.PutUint16(eocd[8:], 500)
	binary.LittleEndian.PutUint16(eocd[10:], 500)
	binary.LittleEndian.PutUint32(eocd[12:], 100)
	binary.LittleEndian.PutUint32(eocd[16:], 0xFFFFFFFF)
	buf.Write(eocd)
	return buf.Bytes()
}

func TestZip64RecordsCannotHideTheRealDirectoryFromTheGuard(t *testing.T) { // fix 6: archive/zip trusts the 64-bit values when the offset is the marker
	for name, n := range map[string]int{"3,000 entries in a small directory": 3000, "30,000 entries in a 1.4 MiB directory": 30000} {
		data := craftedZip64(n)

		var err error
		alloc := allocatedBy(func() { _, err = openImportZip(data, defaultZipLimits()) })

		if err == nil || zipRule(t, err) != domain.RuleZipEntries {
			t.Errorf("%s: err = %v, want zip_entries", name, err)
		}
		if alloc > 1<<20 {
			t.Errorf("%s: rejecting it allocated %d bytes: archive/zip parsed the directory", name, alloc)
		}
	}
}
