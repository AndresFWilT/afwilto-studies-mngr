// Package fsstore stores image bytes on the local filesystem (a named Docker
// volume in the platform), implementing the MediaStorage out-port. It is a
// copy of the certifications manager's DiskFileStorage, adapted to content
// addressing: keys are sharded as ab/cd/<sha256>.
package fsstore

import "errors"

// DiskFileStorage writes under one directory.
type DiskFileStorage struct{ dir string }

func NewDiskFileStorage(dir string) *DiskFileStorage { return &DiskFileStorage{dir: dir} }

var errNotImplemented = errors.New("not implemented")
