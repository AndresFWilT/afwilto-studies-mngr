package in

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// ImportQuestionsCommand carries one import request. The inbound adapter has
// already run stages 1 and 2 (parse, JSON Schema): File is the parsed file, or
// FileErrors holds the failures of the stage that stopped. Primary is the
// `primary` query parameter, "" when absent.
type ImportQuestionsCommand struct {
	ExamID     string
	DryRun     bool
	Primary    string
	File       *domain.QuestionFile
	FileErrors []domain.ImportError
	// Archive is what the .zip the file came in contributes (entry names and
	// the inspected images the questions reference); nil for a .json import.
	Archive *domain.MediaArchive
}

// ImportQuestions runs the rest of the pipeline and, unless DryRun, applies the
// result in one transaction. Errors in the file are not Go errors: they come
// back in Report.Errors with Applied false and nothing written. A Go error is
// domain.ErrNotFound for an exam that is not alive, or an infrastructure failure.
type ImportQuestions interface {
	Execute(ctx context.Context, cmd ImportQuestionsCommand) (domain.ImportReport, error)
}
