package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
)

var errTrailingData = errors.New("trailing data after the JSON value")

// DecodeJSON decodes exactly one JSON value from the body. Anything after it
// is a 422; a body that overflows the limit anywhere — before, inside or after
// the value — is a 413, so the outcome never depends on where the cap falls.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
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
		WriteError(w, http.StatusRequestEntityTooLarge, ErrorBody{
			Code:    "payload_too_large",
			Message: "request body is too large",
		})
		return false
	}
	WriteError(w, http.StatusUnprocessableEntity, ErrorBody{
		Code:    "validation_failed",
		Message: "validation failed",
		Details: []ErrorDetail{{Field: "body", Rule: "json", Message: "request body must be valid JSON"}},
	})
	return false
}

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func IsUUID(s string) bool { return uuidPattern.MatchString(s) }

// PathID returns the {id} path value in canonical (lowercase) form. A
// malformed id can never match a row, so it answers 404 here instead of
// reaching the database.
func PathID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !IsUUID(id) {
		NotFound(w)
		return "", false
	}
	return strings.ToLower(id), true
}
