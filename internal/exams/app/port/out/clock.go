package out

import "time"

// Clock exists so use-case tests are deterministic (fixed instant fakes).
type Clock interface {
	Now() time.Time
}

// IDGenerator supplies UUID v7 identifiers app-side, keeping identity a
// domain concern and tests deterministic.
type IDGenerator interface {
	NewID() string
}
