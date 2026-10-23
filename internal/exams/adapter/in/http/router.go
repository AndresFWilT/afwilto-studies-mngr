package http

import "net/http"

// ImportPattern is the mux pattern of the question import endpoint, the one
// route with its own body cap (MAX_IMPORT_BYTES).
const ImportPattern = "POST /api/v1/exams/{id}/questions/import"

// Deps carries what the exams HTTP adapter needs; use-case ports arrive with
// the slices that implement them.
type Deps struct{}

// Register adds the exams routes to the shared mux. Slice 01 registers none:
// the endpoints are documented in the contract but answer the JSON 404 until
// slices 02–06b implement them.
func Register(Deps) func(*http.ServeMux) {
	return func(*http.ServeMux) {}
}

// BodyLimits maps the import route to its own body cap, for httpx.Config.
func BodyLimits(maxImportBytes int64) map[string]int64 {
	return map[string]int64{ImportPattern: maxImportBytes}
}
