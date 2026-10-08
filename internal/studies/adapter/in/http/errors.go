package http

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"

	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// translateError is the single point where domain errors become HTTP status
// codes; codes never travel further inward.
func translateError(w http.ResponseWriter, err error) {
	var (
		validation *domain.ValidationError
		conflict   *domain.ConflictError
	)
	switch {
	case errors.As(err, &validation):
		details := make([]errorDetail, len(validation.Violations))
		for i, v := range validation.Violations {
			details[i] = errorDetail{Field: v.Field, Rule: v.Rule, Message: v.Message}
		}
		writeError(w, http.StatusUnprocessableEntity, errorBody{
			Code:    "validation_failed",
			Message: "validation failed",
			Details: details,
		})
	case errors.As(err, &conflict):
		writeError(w, http.StatusConflict, errorBody{
			Code:    "conflict",
			Message: conflict.Message,
			Details: []errorDetail{{Field: conflict.Field, Rule: conflict.Rule, Message: conflict.Message}},
		})
	case errors.Is(err, domain.ErrNotFound):
		notFound(w)
	default:
		slog.Error("internal error", "err", err)
		writeError(w, http.StatusInternalServerError, errorBody{
			Code:    "internal",
			Message: "internal server error",
		})
	}
}

func notFound(w http.ResponseWriter) {
	writeError(w, http.StatusNotFound, errorBody{Code: "not_found", Message: "resource not found"})
}

var errTrailingData = errors.New("trailing data after the JSON value")

// decodeJSON decodes exactly one JSON value from the body. Anything after it
// is a 422; a body that overflows the limit anywhere — before, inside or after
// the value — is a 413, so the outcome never depends on where the cap falls.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	err := dec.Decode(dst)
	if err == nil {
		if err = dec.Decode(&struct{}{}); errors.Is(err, io.EOF) {
			return true
		}
		if err == nil {
			err = errTrailingData
		}
	}
	var tooLarge *http.MaxBytesError
	if !errors.As(err, &tooLarge) {
		if _, drainErr := io.Copy(io.Discard, r.Body); errors.As(drainErr, &tooLarge) {
			err = drainErr
		}
	}
	if errors.As(err, &tooLarge) {
		writeError(w, http.StatusRequestEntityTooLarge, errorBody{
			Code:    "payload_too_large",
			Message: "request body is too large",
		})
		return false
	}
	writeError(w, http.StatusUnprocessableEntity, errorBody{
		Code:    "validation_failed",
		Message: "validation failed",
		Details: []errorDetail{{Field: "body", Rule: "json", Message: "request body must be valid JSON"}},
	})
	return false
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func isUUID(s string) bool { return uuidPattern.MatchString(s) }

// pathID returns the {id} path value in canonical (lowercase) form. A
// malformed id can never match a row, so it answers 404 here instead of
// reaching the database.
func pathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !isUUID(id) {
		notFound(w)
		return "", false
	}
	return strings.ToLower(id), true
}

// uuidField canonicalizes an optional id field of a request body; a present
// but malformed value is a 422 on that field.
func uuidField(field string, value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if !isUUID(*value) {
		return nil, domain.SingleViolation(field, "format", field+" must be a UUID")
	}
	canonical := strings.ToLower(*value)
	return &canonical, nil
}
