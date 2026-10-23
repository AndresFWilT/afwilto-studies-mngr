// Package fsstore stores image bytes on the local filesystem (a named Docker
// volume in the platform), implementing the MediaStorage out-port. It is a
// copy of the certifications manager's DiskFileStorage, adapted to content
// addressing: keys are sharded as ab/cd/<sha256>.
package fsstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// DiskFileStorage writes under one directory.
type DiskFileStorage struct{ dir string }

// NewDiskFileStorage also removes the temp files of uploads that crashed
// before their rename (phase-2 fix 7).
func NewDiskFileStorage(dir string) *DiskFileStorage {
	removeStaleUploads(dir, time.Now().Add(-staleUploadAge))
	return &DiskFileStorage{dir: dir}
}

// staleUploadAge is how old an `.upload-*` file must be before no upload can
// still be writing it.
const staleUploadAge = time.Hour

// removeStaleUploads deletes regular files named .upload-* under dir that were
// last modified before cutoff. It is best effort: any failure is ignored, and
// symlinks are neither followed nor removed.
func removeStaleUploads(dir string, cutoff time.Time) {
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || !strings.HasPrefix(d.Name(), ".upload-") {
			return nil
		}
		if info, err := d.Info(); err == nil && info.ModTime().Before(cutoff) {
			_ = os.Remove(path)
		}
		return nil
	})
}

var shaKey = regexp.MustCompile(`^[0-9a-f]{64}$`)

// path maps a hash to ab/cd/<sha256> under the directory. Only a lowercase
// 64-character hex string is a key, so no key can contain a separator or "..".
func (s *DiskFileStorage) path(sha string) (string, error) {
	if !shaKey.MatchString(sha) {
		return "", fmt.Errorf("invalid media key %q: want 64 lowercase hex characters", sha)
	}
	return filepath.Join(s.dir, sha[0:2], sha[2:4], sha), nil
}

// ErrHashMismatch means the bytes given to Put are not the ones the key names.
var ErrHashMismatch = errors.New("content does not match its sha256")

// Put writes atomically (temp file + rename), so a reader never observes a
// half-written image, and verifies the SHA-256 of the bytes before the rename.
// A hash that is already stored is left alone: the content is the same.
func (s *DiskFileStorage) Put(_ context.Context, sha string, content io.Reader) error {
	target, err := s.path(sha)
	if err != nil {
		return err
	}
	if _, err := os.Stat(target); err == nil {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".upload-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }() // a no-op once renamed

	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, hash), content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != sha {
		return ErrHashMismatch
	}
	return os.Rename(tmp.Name(), target)
}

// Open returns the stored bytes; a hash that was never stored satisfies
// errors.Is(err, fs.ErrNotExist).
func (s *DiskFileStorage) Open(_ context.Context, sha string) (io.ReadCloser, error) {
	target, err := s.path(sha)
	if err != nil {
		return nil, err
	}
	return os.Open(target)
}

// Ping checks that the directory exists and accepts writes: the readiness
// probe behind /readyz (S05-5).
func (s *DiskFileStorage) Ping(_ context.Context) error {
	f, err := os.CreateTemp(s.dir, ".ready-*")
	if err != nil {
		return fmt.Errorf("media directory is not writable: %w", err)
	}
	name := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}
