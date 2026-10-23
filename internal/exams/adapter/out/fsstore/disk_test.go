package fsstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var ctx = context.Background()

func sumOf(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

func readAll(t *testing.T, store *DiskFileStorage, sha string) string {
	t.Helper()
	r, err := store.Open(ctx, sha)
	if err != nil {
		t.Fatalf("Open(%s): %v", sha, err)
	}
	defer r.Close()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// files lists every regular file under dir, relative and slash-separated.
func files(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPutThenOpenReturnsTheSameBytes(t *testing.T) { // AC-10, S05-4 storage half
	store := NewDiskFileStorage(t.TempDir())
	sha := sumOf("png bytes")

	if err := store.Put(ctx, sha, strings.NewReader("png bytes")); err != nil {
		t.Fatal(err)
	}

	if got := readAll(t, store, sha); got != "png bytes" {
		t.Errorf("content = %q", got)
	}
}

func TestPutShardsTheFileAsAbCdSha(t *testing.T) { // slice 05 §1.4 keys
	dir := t.TempDir()
	sha := sumOf("sharded")

	if err := NewDiskFileStorage(dir).Put(ctx, sha, strings.NewReader("sharded")); err != nil {
		t.Fatal(err)
	}

	want := sha[0:2] + "/" + sha[2:4] + "/" + sha
	if got := files(t, dir); len(got) != 1 || got[0] != want {
		t.Errorf("files = %v, want [%s]", got, want)
	}
}

func TestPuttingTheSameHashTwiceKeepsOneFileAndNoTemporaries(t *testing.T) { // AC-11 one file
	dir := t.TempDir()
	store := NewDiskFileStorage(dir)
	sha := sumOf("same")

	for i := 0; i < 2; i++ {
		if err := store.Put(ctx, sha, strings.NewReader("same")); err != nil {
			t.Fatalf("put %d: %v", i, err)
		}
	}

	if got := files(t, dir); len(got) != 1 {
		t.Errorf("files = %v, want exactly one", got)
	}
}

func TestPutRefusesBytesThatDoNotHashToTheKey(t *testing.T) { // content addressing integrity
	dir := t.TempDir()

	err := NewDiskFileStorage(dir).Put(ctx, sumOf("expected"), strings.NewReader("something else"))

	if err == nil {
		t.Fatal("a hash mismatch was accepted")
	}
	if got := files(t, dir); len(got) != 0 {
		t.Errorf("files after a refused put = %v", got)
	}
}

type failingReader struct{ after string }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.after != "" {
		n := copy(p, f.after)
		f.after = ""
		return n, nil
	}
	return 0, errors.New("connection reset")
}

func TestAFailedReadLeavesNothingBehind(t *testing.T) {
	dir := t.TempDir()

	err := NewDiskFileStorage(dir).Put(ctx, sumOf("whole"), &failingReader{after: "who"})

	if err == nil {
		t.Fatal("a read error was swallowed")
	}
	if got := files(t, dir); len(got) != 0 {
		t.Errorf("files after a failed put = %v", got)
	}
}

func TestKeysThatAreNotALowercaseSha256CannotReachOutsideTheDirectory(t *testing.T) { // no traversal
	root := t.TempDir()
	dir := filepath.Join(root, "media")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewDiskFileStorage(dir)

	for _, key := range []string{"../escape", "../../etc/passwd", "/abs", "ab/cd/" + sumOf("x"), "", strings.Repeat("A", 64), sumOf("x")[:63], sumOf("x") + "0", `..\x`} {
		if err := store.Put(ctx, key, strings.NewReader("x")); err == nil {
			t.Errorf("Put(%q) succeeded", key)
		}
		if r, err := store.Open(ctx, key); err == nil {
			r.Close()
			t.Errorf("Open(%q) succeeded", key)
		}
	}
	if got := files(t, root); len(got) != 0 {
		t.Errorf("files written by refused keys = %v", got)
	}
}

func TestOpeningAnUnknownHashIsNotExist(t *testing.T) { // S05-4 unknown -> 404 upstream
	_, err := NewDiskFileStorage(t.TempDir()).Open(ctx, sumOf("never stored"))

	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("err = %v, want one satisfying fs.ErrNotExist", err)
	}
}

func TestConcurrentPutsOfOneHashAllSucceed(t *testing.T) { // two imports of the same image
	dir := t.TempDir()
	store := NewDiskFileStorage(dir)
	sha := sumOf("racing")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- store.Put(ctx, sha, strings.NewReader("racing"))
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("put: %v", err)
		}
	}
	if got := files(t, dir); len(got) != 1 {
		t.Errorf("files = %v", got)
	}
	if readAll(t, store, sha) != "racing" {
		t.Error("content corrupted")
	}
}

func TestPingSucceedsOnAWritableDirectoryAndLeavesNoFile(t *testing.T) { // S05-5
	dir := t.TempDir()

	if err := NewDiskFileStorage(dir).Ping(ctx); err != nil {
		t.Fatal(err)
	}

	if got := files(t, dir); len(got) != 0 {
		t.Errorf("files left by Ping = %v", got)
	}
}

func TestPingFailsOnAReadOnlyDirectory(t *testing.T) { // S05-5
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := NewDiskFileStorage(dir).Ping(ctx); err == nil {
		t.Error("Ping succeeded on a read-only directory")
	}
}

func TestPingFailsWhenTheDirectoryIsMissing(t *testing.T) { // the volume is not mounted
	if err := NewDiskFileStorage(filepath.Join(t.TempDir(), "absent")).Ping(ctx); err == nil {
		t.Error("Ping succeeded on a missing directory")
	}
}
