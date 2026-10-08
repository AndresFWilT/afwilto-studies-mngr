package http

import (
	"net/http"
)

// RouterDeps carries everything the HTTP adapter needs, injected from the
// composition root. Use-case ports are added slice by slice (Spec 16 §11).
type RouterDeps struct {
	Pinger       Pinger
	AllowOrigins []string
	MaxBodyBytes int64
}

func NewRouter(deps RouterDeps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", handleHealthz())
	mux.HandleFunc("GET /readyz", handleReadyz(deps.Pinger))

	var handler http.Handler = jsonFallback(mux)
	if deps.MaxBodyBytes > 0 {
		handler = bodyLimitMiddleware(deps.MaxBodyBytes, handler)
	}
	handler = corsMiddleware(deps.AllowOrigins, handler)
	handler = recoverMiddleware(handler)
	handler = requestLogMiddleware(handler)
	return handler
}
