package http

import "errors"

// zipLimits are the bounds of FR-6, a struct so tests can use small ones.
type zipLimits struct {
	MaxEntries  int
	MaxTotal    int64 // uncompressed bytes over all entries
	MaxRootJSON int64
}

func defaultZipLimits() zipLimits {
	return zipLimits{MaxEntries: 1000, MaxTotal: 100 << 20, MaxRootJSON: maxJSONBytes}
}

// importZip is a zip that passed the checks of FR-6 without any entry having
// been decompressed.
type importZip struct{}

func openImportZip(data []byte, limits zipLimits) (*importZip, error) {
	return nil, errors.New("not implemented")
}

// RootJSON returns the questions file; Names lists the other file entries.
func (z *importZip) RootJSON() ([]byte, error) { return nil, errors.New("not implemented") }
func (z *importZip) Names() []string            { return nil }

// ReadEntry decompresses one entry. exceeded is true, with nothing read, when
// the entry declares more than max bytes.
func (z *importZip) ReadEntry(name string, max int64) (data []byte, exceeded bool, err error) {
	return nil, false, errors.New("not implemented")
}
