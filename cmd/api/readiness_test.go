package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/afwilto/studies-mngr/internal/exams/adapter/out/fsstore"
	studieshttp "github.com/afwilto/studies-mngr/internal/studies/adapter/in/http"
)

type dbStub struct{ err error }

func (d dbStub) Ping(context.Context) error { return d.err }

func readyz(db dbStub, mediaDir string) int {
	router := studieshttp.NewRouter(studieshttp.RouterDeps{Pinger: newReadiness(db, fsstore.NewDiskFileStorage(mediaDir))})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	return rec.Code
}

func TestReadyzIs200WhenTheDatabaseAnswersAndMediaDirIsWritable(t *testing.T) {
	if got := readyz(dbStub{}, t.TempDir()); got != http.StatusOK {
		t.Errorf("status = %d, want 200", got)
	}
}

func TestReadyzIs503WhenMediaDirIsReadOnly(t *testing.T) { // S05-5
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if got := readyz(dbStub{}, dir); got != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", got)
	}
}

func TestReadyzIs503WhenMediaDirIsMissing(t *testing.T) { // the volume is not mounted
	if got := readyz(dbStub{}, t.TempDir()+"/absent"); got != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", got)
	}
}

func TestReadyzIs503WhenTheDatabaseIsDown(t *testing.T) {
	if got := readyz(dbStub{err: errors.New("connection refused")}, t.TempDir()); got != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", got)
	}
}
