package http

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/afwilto/studies-mngr/internal/platform/httpx"
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
		details := make([]httpx.ErrorDetail, len(validation.Violations))
		for i, v := range validation.Violations {
			details[i] = httpx.ErrorDetail{Field: v.Field, Rule: v.Rule, Message: v.Message}
		}
		httpx.WriteError(w, http.StatusUnprocessableEntity, httpx.ErrorBody{
			Code:    "validation_failed",
			Message: "validation failed",
			Details: details,
		})
	case errors.As(err, &conflict):
		httpx.WriteError(w, http.StatusConflict, httpx.ErrorBody{
			Code:    "conflict",
			Message: conflict.Message,
			Details: []httpx.ErrorDetail{{Field: conflict.Field, Rule: conflict.Rule, Message: conflict.Message}},
		})
	case errors.Is(err, domain.ErrNotFound):
		httpx.NotFound(w)
	default:
		slog.Error("internal error", "err", err)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.ErrorBody{
			Code:    "internal",
			Message: "internal server error",
		})
	}
}

// uuidField canonicalizes an optional id field of a request body; a present
// but malformed value is a 422 on that field.
func uuidField(field string, value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	if !httpx.IsUUID(*value) {
		return nil, domain.SingleViolation(field, "format", field+" must be a UUID")
	}
	canonical := strings.ToLower(*value)
	return &canonical, nil
}
