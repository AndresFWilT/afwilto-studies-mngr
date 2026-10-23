package testsupport

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"sync"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// FakeMedia implements out.MediaIndex and out.MediaStorage in memory. Rows is
// the media table, Files the stored bytes. The import fake bank registers the
// rows of an applied write (AttachMedia), as the SQL repository does.
type FakeMedia struct {
	mu    sync.Mutex
	Rows  map[string]domain.Media
	Files map[string][]byte
	// Puts counts Put calls (idempotent ones included).
	Puts int
	// PutErr and FindErr, when set, fail the matching call.
	PutErr, FindErr error
	// OnPut runs at the start of every Put, to observe the state of the world
	// at the moment a file is written.
	OnPut func(sha string)
}

func NewFakeMedia() *FakeMedia {
	return &FakeMedia{Rows: map[string]domain.Media{}, Files: map[string][]byte{}}
}

func (m *FakeMedia) Find(_ context.Context, shas []string) (map[string]domain.Media, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.FindErr != nil {
		return nil, m.FindErr
	}
	out := map[string]domain.Media{}
	for _, sha := range shas {
		if row, ok := m.Rows[sha]; ok {
			out[sha] = row
		}
	}
	return out, nil
}

func (m *FakeMedia) Put(_ context.Context, sha string, r io.Reader) error {
	if m.OnPut != nil {
		m.OnPut(sha)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Puts++
	if m.PutErr != nil {
		return m.PutErr
	}
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	m.Files[sha] = data
	return nil
}

func (m *FakeMedia) Open(_ context.Context, sha string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.Files[sha]
	if !ok {
		return nil, fmt.Errorf("media %s: %w", sha, fs.ErrNotExist)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

// Register adds the rows of an applied import write.
func (m *FakeMedia) Register(files []domain.MediaFile) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, f := range files {
		if _, ok := m.Rows[f.Media.SHA256]; !ok {
			m.Rows[f.Media.SHA256] = f.Media
		}
	}
}

// Seed stores an image as an earlier import would have.
func (m *FakeMedia) Seed(media domain.Media, data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Rows[media.SHA256] = media
	m.Files[media.SHA256] = data
}
