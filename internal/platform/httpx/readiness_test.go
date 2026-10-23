package httpx

import (
	"context"
	"errors"
	"testing"
)

type stubPinger struct {
	err   error
	calls int
}

func (s *stubPinger) Ping(context.Context) error { s.calls++; return s.err }

func TestReadinessSucceedsWhenEveryCheckDoes(t *testing.T) {
	a, b := &stubPinger{}, &stubPinger{}

	err := Readiness(Check{"database", a}, Check{"media storage", b}).Ping(context.Background())

	if err != nil || a.calls != 1 || b.calls != 1 {
		t.Errorf("err %v, calls %d/%d; want nil and one call each", err, a.calls, b.calls)
	}
}

func TestReadinessFailsNamingTheFirstFailingComponentAndKeepsTheCause(t *testing.T) { // S05-5
	cause := errors.New("open /data/media/.ready-1: read-only file system")
	ok, bad, never := &stubPinger{}, &stubPinger{err: cause}, &stubPinger{}

	err := Readiness(Check{"database", ok}, Check{"media storage", bad}, Check{"other", never}).Ping(context.Background())

	var notReady *NotReadyError
	if !errors.As(err, &notReady) || notReady.Component != "media storage" || !errors.Is(err, cause) {
		t.Fatalf("err = %v, want a NotReadyError for media storage wrapping the cause", err)
	}
	if never.calls != 0 {
		t.Error("checks after the failing one were still run")
	}
}

func TestReadinessWithNoChecksIsReady(t *testing.T) {
	if err := Readiness().Ping(context.Background()); err != nil {
		t.Error(err)
	}
}
