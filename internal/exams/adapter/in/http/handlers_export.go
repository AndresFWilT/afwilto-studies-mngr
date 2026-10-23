package http

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

// exportEpoch is the modification time of every entry, so that exporting an
// unchanged bank twice yields the same bytes. (The zip format cannot go below 1980.)
var exportEpoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

// handleExportQuestions answers GET /exams/{id}/questions/export (FR-5): a zip
// with questions.json in the FR-2 format and img/<sha256>.<ext> for every image
// the questions use. The zip is built in memory first, so a failure is still a
// proper error response and never a truncated download.
func handleExportQuestions(export in.ExportQuestions, media in.GetMedia) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		examID, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		result, err := export.Execute(r.Context(), examID)
		if err != nil {
			translateError(w, err)
			return
		}
		archive, err := buildExportZip(r.Context(), result, media)
		if err != nil {
			translateError(w, err)
			return
		}
		h := w.Header()
		h.Set("Content-Type", "application/zip")
		h.Set("Content-Disposition", `attachment; filename="questions.zip"`)
		h.Set("Content-Length", fmt.Sprint(len(archive)))
		h.Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(archive)
	}
}

func buildExportZip(ctx context.Context, export domain.QuestionExport, media in.GetMedia) ([]byte, error) {
	paths := map[string]string{} // sha256 -> img/<sha256>.<ext>
	for sha, m := range export.Media {
		ext, ok := domain.MediaExtension(m.ContentType)
		if !ok {
			return nil, fmt.Errorf("stored media %s has the content type %q, which cannot be exported", sha, m.ContentType)
		}
		paths[sha] = "img/" + sha + "." + ext
	}
	questions := make([]domain.Question, len(export.Questions))
	for i, q := range export.Questions {
		questions[i] = domain.RewriteQuestionMedia(q, func(ref string) (string, bool) {
			p, ok := paths[ref]
			return p, ok
		})
	}
	document, err := exportDocument(questions)
	if err != nil {
		return nil, err
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := writeEntry(zw, "questions.json", zip.Deflate, document); err != nil {
		return nil, err
	}
	for _, sha := range domain.QuestionMediaHashes(export.Questions) {
		content, err := media.Execute(ctx, sha)
		if err != nil {
			return nil, fmt.Errorf("exporting media %s: %w", sha, err)
		}
		body, err := io.ReadAll(content.Body)
		content.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading media %s: %w", sha, err)
		}
		if err := writeEntry(zw, paths[sha], zip.Store, body); err != nil { // images are compressed already
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeEntry(zw *zip.Writer, name string, method uint16, data []byte) error {
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: method, Modified: exportEpoch})
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}
