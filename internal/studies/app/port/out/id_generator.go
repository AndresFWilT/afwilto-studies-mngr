package out

// IDGenerator supplies UUID v7 identifiers app-side, keeping identity a
// domain concern and tests deterministic.
type IDGenerator interface {
	NewID() string
}
