package out

import (
	"context"
	"io"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// MediaStorage keeps the bytes of stored images by content hash (Spec 18
// FR-6). Put is idempotent: the same hash is stored once. Open returns an
// error satisfying errors.Is(err, fs.ErrNotExist) for a hash that was never
// stored. Files are never deleted.
type MediaStorage interface {
	// Put stores r under sha, which must be the lowercase hex SHA-256 of the
	// bytes r yields; a mismatch is an error and nothing is stored.
	Put(ctx context.Context, sha string, r io.Reader) error
	Open(ctx context.Context, sha string) (io.ReadCloser, error)
}

// MediaIndex is the table of stored images (migration 0012). Rows are never
// deleted, so a hash found stays found.
type MediaIndex interface {
	// Find returns the stored images among shas, by hash; absent hashes are
	// simply not in the map (a valid empty result, not an error).
	Find(ctx context.Context, shas []string) (map[string]domain.Media, error)
}
