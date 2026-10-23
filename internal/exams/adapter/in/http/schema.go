package http

import "github.com/afwilto/studies-mngr/internal/exams/domain"

// QuestionFileReader runs stages 1 (parse) and 2 (JSON Schema) of the import
// pipeline (slice 03 §1.3). Slice 03 red stub.
type QuestionFileReader struct{}

func NewQuestionFileReader() (*QuestionFileReader, error) { return &QuestionFileReader{}, nil }

func (r *QuestionFileReader) Read(data []byte) (*domain.QuestionFile, []domain.ImportError) {
	return nil, nil
}
