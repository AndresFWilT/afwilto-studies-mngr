package out

import "time"

// Clock exists so use-case tests are deterministic (fixed instant fakes).
type Clock interface {
	Now() time.Time
}
