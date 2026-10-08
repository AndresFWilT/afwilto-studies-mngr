package http

import (
	"net/http"

	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
)

// RouterDeps carries everything the HTTP adapter needs, injected from the
// composition root. Use-case ports are added slice by slice (Spec 16 §11).
type RouterDeps struct {
	Pinger       Pinger
	AllowOrigins []string
	MaxBodyBytes int64

	ListCategories in.ListCategories
	CreateCategory in.CreateCategory
	GetCategory    in.GetCategory
	UpdateCategory in.UpdateCategory
	DeleteCategory in.DeleteCategory
}

func NewRouter(deps RouterDeps) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", handleHealthz())
	mux.HandleFunc("GET /readyz", handleReadyz(deps.Pinger))

	mux.HandleFunc("GET /api/v1/categories", handleListCategories(deps.ListCategories))
	mux.HandleFunc("POST /api/v1/categories", handleCreateCategory(deps.CreateCategory))
	mux.HandleFunc("GET /api/v1/categories/{id}", handleGetCategory(deps.GetCategory))
	mux.HandleFunc("PATCH /api/v1/categories/{id}", handleUpdateCategory(deps.UpdateCategory))
	mux.HandleFunc("DELETE /api/v1/categories/{id}", handleDeleteCategory(deps.DeleteCategory))

	var handler http.Handler = jsonFallback(mux)
	if deps.MaxBodyBytes > 0 {
		handler = bodyLimitMiddleware(deps.MaxBodyBytes, handler)
	}
	handler = corsMiddleware(deps.AllowOrigins, handler)
	handler = recoverMiddleware(handler)
	handler = requestLogMiddleware(handler)
	return handler
}
