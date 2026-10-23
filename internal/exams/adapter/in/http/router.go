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

	// Slice 04: the bank browser, the single-question endpoints and the
	// category merge. Each route exists when its use case is wired.
	ListExamQuestions in.ListExamQuestions
	GetQuestion       in.GetQuestion
	DeleteQuestion    in.DeleteQuestion
	MergeCategories   in.MergeCategories

	// Slice 05: image serving and the question export. The media route needs
	// GetMedia; the export needs both, because it streams the images it holds.
	GetMedia        in.GetMedia
	ExportQuestions in.ExportQuestions
}

// Register adds the exams routes to the shared mux: the exams CRUD (slice 02),
// the question import (slice 03) and the bank browser, question and merge
// endpoints (slice 04), each when its dependencies are wired. The other
// documented endpoints answer the JSON 404 until slices 05–06b.
func Register(deps Deps) func(*http.ServeMux) {
	return func(mux *http.ServeMux) {
		mux.HandleFunc("GET /api/v1/subjects/{id}/exams", handleListSubjectExams(deps.ListSubjectExams))
		mux.HandleFunc("POST /api/v1/subjects/{id}/exams", handleCreateExam(deps.CreateExam))
		mux.HandleFunc("GET /api/v1/exams/{id}", handleGetExam(deps.GetExam))
		mux.HandleFunc("PATCH /api/v1/exams/{id}", handleUpdateExam(deps.UpdateExam))
		mux.HandleFunc("DELETE /api/v1/exams/{id}", handleDeleteExam(deps.DeleteExam))
		if deps.ImportQuestions != nil && deps.FileReader != nil {
			mux.HandleFunc(ImportPattern, handleImportQuestions(deps.ImportQuestions, deps.FileReader))
		}
		if deps.ListExamQuestions != nil {
			mux.HandleFunc("GET /api/v1/exams/{id}/questions", handleListExamQuestions(deps.ListExamQuestions))
		}
		if deps.GetQuestion != nil {
			mux.HandleFunc("GET /api/v1/questions/{id}", handleGetQuestion(deps.GetQuestion))
		}
		if deps.DeleteQuestion != nil {
			mux.HandleFunc("DELETE /api/v1/questions/{id}", handleDeleteQuestion(deps.DeleteQuestion))
		}
		if deps.MergeCategories != nil {
			mux.HandleFunc("POST /api/v1/exams/{id}/categories/merge", handleMergeCategories(deps.MergeCategories))
		}
	}
}

// BodyLimits maps the import route to its own body cap, for httpx.Config.
func BodyLimits(maxImportBytes int64) map[string]int64 {
	return map[string]int64{ImportPattern: maxImportBytes}
}
