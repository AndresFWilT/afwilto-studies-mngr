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

// Phase-3 fix 7: the backstop names the field it knows, and `body` otherwise.

func invalidTextField(t *testing.T, err error) string {
	t.Helper()
	var v *domain.ValidationError
	if !errors.As(err, &v) || len(v.Violations) != 1 || v.Violations[0].Rule != domain.RuleInvalidText {
		t.Fatalf("got %v, want one invalid_text violation", err)
	}
	return v.Violations[0].Field
}

func TestEditWriteErrorNamesBodyWhenItDoesNotKnowTheField(t *testing.T) { // phase-3 fix 7
	got := editWriteError(fmt.Errorf("some statement: %w", &pgconn.PgError{Code: "22021"}))

	if field := invalidTextField(t, got); field != "body" {
		t.Errorf("field = %q, want body", field)
	}
}

func TestEditWriteErrorNamesTheFieldOfTheStatementThatRefusedTheText(t *testing.T) { // phase-3 fix 7, real Postgres
	nul := "bad\x00name"
	exam := domain.Exam{ID: "0abcdef0-0000-7000-8000-00000000e001", Version: 2, Name: domain.RehydrateExamName(nul)}
	cases := map[string]struct {
		write domain.EditWrite
		field string
	}{
		"an exam name":          {domain.EditWrite{ExamID: exam.ID, Exam: &exam, ExpectedVersion: 1}, "name"},
		"a classification name": {domain.EditWrite{ExamID: exam.ID, Classifications: []domain.Classification{{ID: "0abcdef0-0000-7000-8000-00000000c001", Name: nul, Fold: "x"}}}, "classifications"},
		"a category name":       {domain.EditWrite{ExamID: exam.ID, Categories: []domain.Category{{ID: "0abcdef0-0000-7000-8000-00000000c002", Name: nul, Fold: "y", Key: "k"}}}, "categories"},
	}
	for label, tc := range cases {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		werr := writeEdit(ctx, tx, domain.Bank{}, tc.write)
		_ = tx.Rollback(ctx)

		if werr == nil {
			t.Fatalf("%s: Postgres accepted the NUL", label)
		}
		if field := invalidTextField(t, editWriteError(werr)); field != tc.field {
			t.Errorf("%s: field = %q, want %q", label, field, tc.field)
		}
	}
}
