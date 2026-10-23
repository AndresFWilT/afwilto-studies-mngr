package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// Phase-2 fix 5: a text the database refuses on an edit is 422 invalid_text,
// never a 500 (defense in depth: the domain rejects NUL names as `format` first).

func TestEditWriteErrorMapsRefusedTextToAnInvalidTextValidation(t *testing.T) { // fix 5
	for name, code := range map[string]string{"text with a NUL (22021)": "22021", "jsonb with a NUL (22P05)": "22P05"} {
		got := editWriteError(fmt.Errorf("edit statement 3: %w", &pgconn.PgError{Code: code}))

		var v *domain.ValidationError
		if !errors.As(got, &v) || len(v.Violations) != 1 || v.Violations[0].Rule != domain.RuleInvalidText {
			t.Errorf("%s: got %v, want a validation error with rule invalid_text", name, got)
		}
	}
}

func TestEditWriteErrorLeavesOtherErrorsAlone(t *testing.T) { // fix 5: no over-broad mapping
	other := &pgconn.PgError{Code: "22003"}

	if got := editWriteError(other); !errors.Is(got, other) {
		t.Errorf("got %v, want the error untouched", got)
	}
}
