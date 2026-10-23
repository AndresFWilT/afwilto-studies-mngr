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

type readiness []Check

// Readiness is a Pinger that succeeds when every check does. The first check
// to fail ends it with a *NotReadyError naming that component.
func Readiness(checks ...Check) Pinger { return readiness(checks) }

func (r readiness) Ping(ctx context.Context) error {
	for _, c := range r {
		if err := c.Pinger.Ping(ctx); err != nil {
			return &NotReadyError{Component: c.Name, Err: err}
		}
	}
	return nil
}
