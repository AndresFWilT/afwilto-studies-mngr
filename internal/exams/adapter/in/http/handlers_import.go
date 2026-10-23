package http

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

// The size limits of an upload (Spec 18 FR-4); the route's overall body cap,
// MAX_IMPORT_BYTES, sits above both.
const (
	maxJSONBytes = 10 << 20
	maxZipBytes  = 50 << 20
)

const maxPrimaryRunes = 40

type importParams struct {
	dryRun  bool
	primary string
}

// parseImportParams reads ?dryRun=true|false (default false) and ?primary=.
func parseImportParams(r *http.Request) (importParams, []domain.Violation) {
	var p importParams
	var violations []domain.Violation
	q := r.URL.Query()
	if q.Has("dryRun") {
		switch q.Get("dryRun") {
		case "true":
			p.dryRun = true
		case "false":
		default:
			violations = append(violations, domain.Violation{Field: "dryRun", Rule: "format", Message: "dryRun must be true or false"})
		}
	}
	if q.Has("primary") {
		name := strings.TrimSpace(q.Get("primary"))
		switch {
		case name == "":
			violations = append(violations, domain.Violation{Field: "primary", Rule: "required", Message: "primary must not be empty"})
		case utf8.RuneCountInString(name) > maxPrimaryRunes:
			violations = append(violations, domain.Violation{Field: "primary", Rule: "max_length", Message: "primary must be at most 40 characters"})
		default:
			p.primary = name
		}
	}
	return p, violations
}

// uploadedFile is what readUpload found; exactly one of the fields is set.
type uploadedFile struct {
	data []byte
	err  error // *http.MaxBytesError or a domain.ValidationError
}

// readUpload streams the multipart body up to the `file` part and reads at
// most maxJSONBytes+1 bytes of it, so an oversized upload is noticed without
// being held in memory.
func readUpload(r *http.Request) uploadedFile {
	missing := domain.SingleViolation("file", "required", "a multipart/form-data upload with a `file` part is required")
	reader, err := r.MultipartReader()
	if err != nil {
		return uploadedFile{err: missing}
	}
	for {
		part, err := reader.NextPart()
		if err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				return uploadedFile{err: tooLarge}
			}
			return uploadedFile{err: missing}
		}
		if part.FormName() != "file" {
			continue
		}
		data, err := readPart(part)
		if err != nil {
			return uploadedFile{err: err}
		}
		return uploadedFile{data: data}
	}
}

// readPart reads the part up to the limit of its kind, plus one byte so that an
// oversized file is noticed: a zip (by its magic) may be 50 MiB, anything else 10.
func readPart(part *multipart.Part) ([]byte, error) {
	body := bufio.NewReader(part)
	head, _ := body.Peek(len(zipMagic))
	limit := int64(maxJSONBytes)
	if isZip(head) {
		limit = maxZipBytes
	}
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return nil, tooLarge
		}
		return nil, domain.SingleViolation("file", "required", "the `file` part could not be read")
	}
	return data, nil
}

// looksLikeJSON is the file type detection of slice 03 §1.2: the first byte
// that is not whitespace (after an optional UTF-8 byte order mark) is `{`.
func looksLikeJSON(data []byte) bool {
	data = bytes.TrimPrefix(data, utf8BOM)
	trimmed := bytes.TrimLeft(data, " \t\r\n")
	return len(trimmed) > 0 && trimmed[0] == '{'
}

func payloadTooLarge(w http.ResponseWriter, message string) {
	httpx.WriteError(w, http.StatusRequestEntityTooLarge, httpx.ErrorBody{Code: "payload_too_large", Message: message})
}

func handleImportQuestions(imp in.ImportQuestions, reader *QuestionFileReader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		examID, ok := httpx.PathID(w, r)
		if !ok {
			return
		}
		params, violations := parseImportParams(r)
		if len(violations) > 0 {
			translateError(w, domain.NewValidation(violations...))
			return
		}

		upload := readUpload(r)
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(upload.err, &tooLarge):
			payloadTooLarge(w, "request body is too large")
			return
		case upload.err != nil:
			translateError(w, upload.err)
			return
		case isZip(upload.data) && len(upload.data) > maxZipBytes:
			payloadTooLarge(w, "a .zip import file may be at most 50 MiB")
			return
		case !isZip(upload.data) && !looksLikeJSON(upload.data):
			translateError(w, domain.SingleViolation("file", domain.RuleFileType,
				"only a .json file (a JSON object) or a .zip is accepted"))
			return
		case !isZip(upload.data) && len(upload.data) > maxJSONBytes:
			payloadTooLarge(w, "a .json import file may be at most 10 MiB")
			return
		}

		cmd := in.ImportQuestionsCommand{ExamID: examID, DryRun: params.dryRun, Primary: params.primary}
		jsonBytes := upload.data
		var archive *importZip
		if isZip(upload.data) {
			var err error
			if archive, err = openImportZip(upload.data, defaultZipLimits()); err != nil {
				translateError(w, err)
				return
			}
			if jsonBytes, err = archive.RootJSON(); err != nil {
				translateError(w, err)
				return
			}
		}
		cmd.File, cmd.FileErrors = reader.Read(jsonBytes)
		if archive != nil && cmd.File != nil {
			var err error
			if cmd.Archive, err = archiveForQuestions(archive, cmd.File.Questions); err != nil {
				translateError(w, err)
				return
			}
		}
		report, err := imp.Execute(r.Context(), cmd)
		if err != nil {
			translateError(w, err)
			return
		}
		status := http.StatusOK
		if len(report.Errors) > 0 {
			status = http.StatusUnprocessableEntity // the report itself is the body
		}
		httpx.WriteData(w, status, toImportReportDTO(report))
	}
}
