// Package http is the exams bounded context's inbound HTTP adapter.
package http

import "net/http"

// ImportPattern is the mux pattern of the question import endpoint.
const ImportPattern = ""

// Deps carries what the exams HTTP adapter needs; use-case ports arrive with
// the slices that implement them.
type Deps struct{}

// Register adds the exams routes to the shared mux.
func Register(Deps) func(*http.ServeMux) {
	return func(*http.ServeMux) {}
}

// BodyLimits maps the import route to its own body cap.
func BodyLimits(maxImportBytes int64) map[string]int64 {
	return nil
}
