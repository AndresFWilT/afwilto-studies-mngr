package http

import (
	"net/http"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
)

// ImportPattern is the mux pattern of the question import endpoint, the one
// route with its own body cap (MAX_IMPORT_BYTES).
const ImportPattern = "POST /api/v1/exams/{id}/questions/import"

// Deps carries what the exams HTTP adapter needs, injected from the
// composition root; use-case ports arrive with the slices that implement them.
type Deps struct {
	CreateExam       in.CreateExam
	ListSubjectExams in.ListSubjectExams
	GetExam          in.GetExam
	UpdateExam       in.UpdateExam
	DeleteExam       in.DeleteExam

	// Slice 03: the question import. Both are needed for the route to exist.
	ImportQuestions in.ImportQuestions
	FileReader      *QuestionFileReader
}

// Register adds the exams routes to the shared mux. Slice 02 registers the
// exams CRUD; the other documented endpoints answer the JSON 404 until slices
// 03–06b implement them.
func Register(deps Deps) func(*http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/v1/subjects/{id}/exams", handleListSubjectExams(deps.ListSubjectExams))
		mux.HandleFunc("POST /api/v1/subjects/{id}/exams", handleCreateExam(deps.CreateExam))
		mux.HandleFunc("GET /api/v1/exams/{id}", handleGetExam(deps.GetExam))
		mux.HandleFunc("PATCH /api/v1/exams/{id}", handleUpdateExam(deps.UpdateExam))
		mux.HandleFunc("DELETE /api/v1/exams/{id}", handleDeleteExam(deps.DeleteExam))
	}
}

// BodyLimits maps the import route to its own body cap, for httpx.Config.
func BodyLimits(maxImportBytes int64) map[string]int64 {
	return map[string]int64{ImportPattern: maxImportBytes}
}
