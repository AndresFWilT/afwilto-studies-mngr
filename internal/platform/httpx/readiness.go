package httpx

import (
	"context"
	"fmt"
)

// Pinger reports whether one dependency of the service is usable.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Check names a dependency whose readiness /readyz reports.
type Check struct {
	Name   string
	Pinger Pinger
}

// NotReadyError says which component failed its check. Its message is safe
// to show to a caller: the cause (a path, a connection string) stays in Err.
type NotReadyError struct {
	Component string
	Err       error
}

func (e *NotReadyError) Error() string { return fmt.Sprintf("%s unavailable: %v", e.Component, e.Err) }
func (e *NotReadyError) Unwrap() error { return e.Err }

type allReady struct{}

func (allReady) Ping(context.Context) error { return nil }

// Readiness is a Pinger that succeeds when every check does.
func Readiness(checks ...Check) Pinger { return allReady{} }
