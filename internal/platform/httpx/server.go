package httpx

import "net/http"

// Config carries the cross-cutting HTTP settings every context shares.
type Config struct {
	AllowOrigins []string
	// MaxBodyBytes caps request bodies; 0 disables the cap.
	MaxBodyBytes int64
	// BodyLimits overrides MaxBodyBytes per mux pattern ("POST /path/{id}").
	BodyLimits map[string]int64
}

// NewServer builds the mux, lets every registrar add its routes and wraps the
// result in the middleware chain: request log → recover → CORS → body limit →
// JSON 404/405 fallback.
func NewServer(cfg Config, registrars ...func(*http.ServeMux)) http.Handler {
	mux := http.NewServeMux()
	for _, register := range registrars {
		register(mux)
	}

	var handler http.Handler = JSONFallback(mux)
	if cfg.MaxBodyBytes > 0 {
		handler = BodyLimit(cfg.MaxBodyBytes, handler)
	}
	handler = CORS(cfg.AllowOrigins, handler)
	handler = Recover(handler)
	handler = RequestLog(handler)
	return handler
}
