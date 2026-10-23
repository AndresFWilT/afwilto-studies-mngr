package http

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// zipLimits are the bounds of FR-6, a struct so tests can use small ones.
type zipLimits struct {
	MaxEntries  int
	MaxTotal    int64 // uncompressed bytes over all entries
	MaxRootJSON int64
}

func defaultZipLimits() zipLimits {
	return zipLimits{MaxEntries: 1000, MaxTotal: 100 << 20, MaxRootJSON: maxJSONBytes}
}

// zipMagic is what a zip upload starts with (a local file header).
var zipMagic = []byte("PK\x03\x04")

func isZip(data []byte) bool { return bytes.HasPrefix(data, zipMagic) }

func zipViolation(rule, format string, args ...any) error {
	return domain.SingleViolation("file", rule, fmt.Sprintf(format, args...))
}

// importZip is a zip that passed the checks of FR-6 without any entry having
// been decompressed: they look at the end record, the central directory
// (names, modes and declared sizes) and nothing else.
type importZip struct {
	files    map[string]*zip.File
	rootJSON *zip.File
	names    []string
	limits   zipLimits
}

// openImportZip applies the checks in a fixed order, and the first failing
// one decides the answer: entry count, declared total size, entry names,
// layout. Every failure is a 422 on the `file` field.
func openImportZip(data []byte, limits zipLimits) (*importZip, error) {
	// The end record holds the entry count. Reading it first keeps a zip with
	// millions of empty entries from making archive/zip build a huge directory.
	if n, ok := endRecordCount(data); ok && n > limits.MaxEntries {
		return nil, zipViolation(domain.RuleZipEntries, "the zip has %d entries or more; the limit is %d", n, limits.MaxEntries)
	}
	if err := guardCentralDirectory(data, limits.MaxEntries); err != nil {
		return nil, err
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) { // insecure names are the next checks' job
		return nil, zipViolation(domain.RuleFileType, "the file starts like a zip but could not be read as one: %v", err)
	}
	if len(zr.File) > limits.MaxEntries {
		return nil, zipViolation(domain.RuleZipEntries, "the zip has %d entries; the limit is %d", len(zr.File), limits.MaxEntries)
	}

	var total uint64
	for _, f := range zr.File {
		if f.UncompressedSize64 > uint64(limits.MaxTotal) || total+f.UncompressedSize64 > uint64(limits.MaxTotal) {
			return nil, zipViolation(domain.RuleZipSize, "the zip declares more than %d bytes uncompressed (entry %q)", limits.MaxTotal, f.Name)
		}
		total += f.UncompressedSize64
	}

	z := &importZip{files: map[string]*zip.File{}, limits: limits}
	for _, f := range zr.File {
		if msg := badEntryName(f); msg != "" {
			return nil, zipViolation(domain.RuleZipPath, "%s", msg)
		}
		if _, dup := z.files[f.Name]; dup {
			return nil, zipViolation(domain.RuleZipPath, "the zip has two entries named %q", f.Name)
		}
		z.files[f.Name] = f
	}

	var roots []*zip.File
	for _, f := range zr.File {
		if isDirEntry(f) {
			continue
		}
		if !strings.Contains(f.Name, "/") && strings.EqualFold(path.Ext(f.Name), ".json") {
			roots = append(roots, f)
		}
	}
	if len(roots) != 1 {
		return nil, zipViolation(domain.RuleZipLayout, "the zip must hold exactly one .json file at its root; it has %d", len(roots))
	}
	z.rootJSON = roots[0]

	for _, f := range zr.File {
		if f != z.rootJSON && !isDirEntry(f) {
			z.names = append(z.names, f.Name)
		}
	}
	sort.Strings(z.names)
	return z, nil
}

func isDirEntry(f *zip.File) bool { return strings.HasSuffix(f.Name, "/") || f.Mode().IsDir() }

// badEntryName explains why an entry must not be accepted, "" when it is fine:
// an absolute or drive path, a backslash, a NUL, a ".." segment, or a mode
// that is not a regular file or a directory (symlinks above all).
func badEntryName(f *zip.File) string {
	name := f.Name
	switch {
	case strings.ContainsRune(name, 0):
		return fmt.Sprintf("entry %q has a NUL in its name", name)
	case strings.Contains(name, `\`):
		return fmt.Sprintf("entry %q has a backslash in its name", name)
	case strings.HasPrefix(name, "/"):
		return fmt.Sprintf("entry %q is an absolute path", name)
	case len(name) >= 2 && name[1] == ':' && (name[0]|0x20 >= 'a' && name[0]|0x20 <= 'z'):
		return fmt.Sprintf("entry %q starts with a drive letter", name)
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == ".." {
			return fmt.Sprintf("entry %q climbs out of the archive with ..", name)
		}
	}
	if mode := f.Mode(); !mode.IsRegular() && !mode.IsDir() {
		return fmt.Sprintf("entry %q is not a regular file (mode %s); symlinks and special files are refused", name, mode.Type())
	}
	return ""
}

// endRecord holds the fields of the end-of-central-directory record that the
// guards read.
type endRecord struct {
	pos    int    // where the record starts
	count  uint16 // entries, 16 bits (0xFFFF is the zip64 marker)
	size   uint32 // bytes of the central directory
	offset uint32 // where the central directory starts
}

const (
	endRecordLen       = 22
	endRecordSignature = 0x06054b50
)

// findEndRecord locates the last end-of-central-directory record, the way
// archive/zip does (the first signature from the end, within the 64 KiB a
// comment can occupy); ok is false when there is none (archive/zip then
// reports the damage).
func findEndRecord(data []byte) (rec endRecord, ok bool) {
	for i := len(data) - endRecordLen; i >= 0 && i >= len(data)-endRecordLen-0xFFFF; i-- {
		if binary.LittleEndian.Uint32(data[i:]) == endRecordSignature {
			return endRecord{
				pos:    i,
				count:  binary.LittleEndian.Uint16(data[i+10:]),
				size:   binary.LittleEndian.Uint32(data[i+12:]),
				offset: binary.LittleEndian.Uint32(data[i+16:]),
			}, true
		}
	}
	return endRecord{}, false
}

// endRecordCount reads the total entry count of the end-of-central-directory
// record without parsing the directory; ok is false when no record is found
// (archive/zip then reports the damage).
func endRecordCount(data []byte) (count int, ok bool) {
	rec, ok := findEndRecord(data)
	return int(rec.count), ok
}

// RootJSON returns the questions file; a file over the .json limit is zip_size.
func (z *importZip) RootJSON() ([]byte, error) {
	return z.read(z.rootJSON, z.limits.MaxRootJSON)
}

// Names lists the file entries other than the root JSON, sorted.
func (z *importZip) Names() []string { return z.names }

// ReadEntry decompresses one entry. exceeded is true, with nothing read, when
// the entry declares more than max bytes.
func (z *importZip) ReadEntry(name string, max int64) (data []byte, exceeded bool, err error) {
	f, ok := z.files[name]
	if !ok {
		return nil, false, fmt.Errorf("the zip has no entry %q", name)
	}
	if f.UncompressedSize64 > uint64(max) {
		return nil, true, nil
	}
	data, err = z.read(f, max)
	return data, false, err
}

// read counts what an entry really yields. Its header said how much that
// would be; one byte more means the header lied (zip_size).
func (z *importZip) read(f *zip.File, max int64) ([]byte, error) {
	if f.UncompressedSize64 > uint64(max) {
		return nil, zipViolation(domain.RuleZipSize, "entry %q declares %d bytes; at most %d are accepted here", f.Name, f.UncompressedSize64, max)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, zipViolation(domain.RuleFileType, "entry %q could not be opened: %v", f.Name, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, int64(f.UncompressedSize64)+1))
	// One byte more than declared is a lie, whether archive/zip notices it
	// first (ErrFormat) or our own count does.
	if uint64(len(data)) > f.UncompressedSize64 || errors.Is(err, zip.ErrFormat) {
		return nil, zipViolation(domain.RuleZipSize, "entry %q holds more bytes than the %d its header declares", f.Name, f.UncompressedSize64)
	}
	if err != nil {
		return nil, zipViolation(domain.RuleFileType, "entry %q could not be decompressed: %v", f.Name, err)
	}
	return data, nil
}

// maxCentralDirectoryBytes is the largest central directory the guard reads
// (phase-2 fix 6); 1,000 entries with the longest ordinary names fit in a
// fraction of it.
const maxCentralDirectoryBytes = 1 << 20

const (
	centralHeaderLen       = 46
	centralHeaderSignature = 0x02014b50
	zip64LocatorLen        = 20
	zip64LocatorSignature  = 0x07064b50
	zip64RecordSignature   = 0x06064b50
)

// guardCentralDirectory decides the entry count without archive/zip: the
// end record's count field is only 16 bits, and archive/zip accepts a
// directory whose real count differs from it by a multiple of 65,536 (and
// takes the 64-bit zip64 values instead when a marker says so), parsing
// every entry. It rejects (zip_entries) a central directory over 1 MiB, then
// walks the directory headers and rejects more than maxEntries. Input it
// cannot find a directory in is left to archive/zip.
func guardCentralDirectory(data []byte, maxEntries int) error {
	rec, ok := findEndRecord(data)
	if !ok {
		return nil
	}
	size, offset, end := uint64(rec.size), uint64(rec.offset), uint64(rec.pos)
	// archive/zip's own triggers for reading the zip64 record (including its
	// comparison of the size with 0xFFFF)
	if rec.count == 0xFFFF || rec.size == 0xFFFF || rec.size == 0xFFFFFFFF || rec.offset == 0xFFFFFFFF {
		if z, found := readZip64Directory(data, rec.pos); found {
			size, offset, end = z.size, z.offset, z.pos
		}
	}
	if size > maxCentralDirectoryBytes {
		return zipViolation(domain.RuleZipEntries, "the zip's central directory is %d bytes; at most %d are read", size, maxCentralDirectoryBytes)
	}

	// archive/zip starts at the stated offset, or where the directory must
	// begin if the archive has bytes in front of it.
	start := -1
	for _, candidate := range []uint64{offset, end - min(end, size)} {
		if candidate <= uint64(len(data)) && uint64(len(data))-candidate >= centralHeaderLen && binary.LittleEndian.Uint32(data[candidate:]) == centralHeaderSignature {
			start = int(candidate)
			break
		}
	}
	if start < 0 {
		return nil
	}
	// archive/zip reads headers until one is not a header, whatever the end
	// record says the directory's size is, so the walk does too; it stops as
	// soon as the limit is passed.
	entries := 0
	for pos := start; pos+centralHeaderLen <= len(data) && binary.LittleEndian.Uint32(data[pos:]) == centralHeaderSignature; {
		entries++
		if entries > maxEntries {
			return zipViolation(domain.RuleZipEntries, "the zip's central directory lists more than %d entries", maxEntries)
		}
		pos += centralHeaderLen + int(binary.LittleEndian.Uint16(data[pos+28:])) + int(binary.LittleEndian.Uint16(data[pos+30:])) + int(binary.LittleEndian.Uint16(data[pos+32:]))
	}
	return nil
}

type zip64Directory struct {
	pos, size, offset uint64
}

// readZip64Directory follows the zip64 locator that sits just before the end
// record to the zip64 end record, as archive/zip does.
func readZip64Directory(data []byte, endPos int) (z zip64Directory, ok bool) {
	loc := endPos - zip64LocatorLen
	if loc < 0 || binary.LittleEndian.Uint32(data[loc:]) != zip64LocatorSignature {
		return zip64Directory{}, false
	}
	pos := binary.LittleEndian.Uint64(data[loc+8:])
	if pos > uint64(len(data)) || uint64(len(data))-pos < 56 || binary.LittleEndian.Uint32(data[pos:]) != zip64RecordSignature {
		return zip64Directory{}, false
	}
	return zip64Directory{pos: pos, size: binary.LittleEndian.Uint64(data[pos+40:]), offset: binary.LittleEndian.Uint64(data[pos+48:])}, true
}
