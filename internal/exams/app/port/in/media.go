package in

import (
	"context"
	"io"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// MediaContent is a stored image ready to be served; the caller closes Body.
type MediaContent struct {
	Media domain.Media
	Body  io.ReadCloser
}

// GetMedia opens a stored image by content hash (FR-6 serving).
// domain.ErrNotFound for a malformed or unknown hash; a stored row whose file
// is missing is an infrastructure error, not a 404.
type GetMedia interface {
	Execute(ctx context.Context, sha string) (MediaContent, error)
}

// ExportQuestions reads the alive questions of an exam and the images they
// use (FR-5); domain.ErrNotFound when the exam is not alive. The inbound
// adapter turns the result into the zip.
type ExportQuestions interface {
	Execute(ctx context.Context, examID string) (domain.QuestionExport, error)
}
