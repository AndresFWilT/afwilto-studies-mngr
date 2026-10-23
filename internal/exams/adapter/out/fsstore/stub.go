package fsstore

import (
	"context"
	"io"
)

func (s *DiskFileStorage) Put(_ context.Context, sha string, r io.Reader) error { return errNotImplemented }

func (s *DiskFileStorage) Open(_ context.Context, sha string) (io.ReadCloser, error) {
	return nil, errNotImplemented
}

// Ping checks that the directory accepts writes (readiness, S05-5).
func (s *DiskFileStorage) Ping(_ context.Context) error { return errNotImplemented }
