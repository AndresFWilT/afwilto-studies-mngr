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

	ListSubjects  in.ListSubjects
	CreateSubject in.CreateSubject
	GetSubject    in.GetSubject
	UpdateSubject in.UpdateSubject
	DeleteSubject in.DeleteSubject

	ListNoteKinds  in.ListNoteKinds
	CreateNoteKind in.CreateNoteKind
	GetNoteKind    in.GetNoteKind
	RenameNoteKind in.RenameNoteKind
	DeleteNoteKind in.DeleteNoteKind
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

	mux.HandleFunc("GET /api/v1/note-kinds", handleListNoteKinds(deps.ListNoteKinds))
	mux.HandleFunc("POST /api/v1/note-kinds", handleCreateNoteKind(deps.CreateNoteKind))
	mux.HandleFunc("GET /api/v1/note-kinds/{id}", handleGetNoteKind(deps.GetNoteKind))
	mux.HandleFunc("PATCH /api/v1/note-kinds/{id}", handleRenameNoteKind(deps.RenameNoteKind))
	mux.HandleFunc("DELETE /api/v1/note-kinds/{id}", handleDeleteNoteKind(deps.DeleteNoteKind))

	mux.HandleFunc("GET /api/v1/subjects", handleListSubjects(deps.ListSubjects))
	mux.HandleFunc("POST /api/v1/subjects", handleCreateSubject(deps.CreateSubject))
	mux.HandleFunc("GET /api/v1/subjects/{id}", handleGetSubject(deps.GetSubject))
	mux.HandleFunc("PATCH /api/v1/subjects/{id}", handleUpdateSubject(deps.UpdateSubject))
	mux.HandleFunc("DELETE /api/v1/subjects/{id}", handleDeleteSubject(deps.DeleteSubject))

	var handler http.Handler = jsonFallback(mux)
	if deps.MaxBodyBytes > 0 {
		handler = bodyLimitMiddleware(deps.MaxBodyBytes, handler)
	}
	handler = corsMiddleware(deps.AllowOrigins, handler)
	handler = recoverMiddleware(handler)
	handler = requestLogMiddleware(handler)
	return handler
}
