package domain

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrNotFoundSurvivesWrapping(t *testing.T) {
	if !errors.Is(fmt.Errorf("loading exam: %w", ErrNotFound), ErrNotFound) {
		t.Error("errors.Is(wrapped, ErrNotFound) = false, want true")
	}
	if ErrNotFound.Error() != "not found" {
		t.Errorf("ErrNotFound = %q, want %q", ErrNotFound.Error(), "not found")
	}
}

func TestValidationErrorListsEveryViolation(t *testing.T) {
	err := NewValidation(
		Violation{Field: "questionCount", Rule: RuleRange, Message: "must be 1..200"},
		Violation{Field: "passPercent", Rule: RuleRange, Message: "must be 1..100"},
	)

	want := "validation failed: questionCount: must be 1..200; passPercent: must be 1..100"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestSingleViolationCarriesFieldRuleAndMessage(t *testing.T) {
	err := SingleViolation("name", RuleDuplicateKey, "key repeated")

	if len(err.Violations) != 1 || err.Violations[0] != (Violation{Field: "name", Rule: "duplicate_key", Message: "key repeated"}) {
		t.Errorf("Violations = %+v", err.Violations)
	}
}

func TestConflictErrorCarriesRuleAndMessage(t *testing.T) {
	err := NewConflict("name", RuleDuplicateName, "an exam with this name exists")

	if err.Field != "name" || err.Rule != "duplicate_name" || err.Message != "an exam with this name exists" {
		t.Errorf("conflict = %+v", err)
	}
	if want := "conflict: duplicate_name: an exam with this name exists"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	var target *ConflictError
	if !errors.As(fmt.Errorf("wrapped: %w", err), &target) {
		t.Error("errors.As(*ConflictError) = false through a wrap")
	}
}

// Parent §6: the closed set of rules, as the wire strings the API returns.
// A typo here would silently break the contract the frontend codes against.
func TestRuleConstantsAreTheWireStringsOfSpec18(t *testing.T) {
	cases := []struct{ got, want string }{
		{RuleDuplicateName, "duplicate_name"},
		{RuleInUse, "in_use"},
		{RuleStaleVersion, "stale_version"},
		{RuleRange, "range"},
		{RuleJSON, "json"},
		{RuleSchema, "schema"},
		{RuleFileType, "file_type"},
		{RuleTooManyClassifications, "too_many_classifications"},
		{RulePrimarySingleValued, "primary_single_valued"},
		{RuleCorrectCount, "correct_count"},
		{RuleOptionContent, "option_content"},
		{RuleDuplicateKey, "duplicate_key"},
		{RulePattern, "pattern"},
		{RuleMediaMissing, "media_missing"},
		{RuleWeightsSum, "weights_sum"},
		{RulePacePair, "pace_pair"},
		{RuleMergeScope, "merge_scope"},
		{RuleZipEntries, "zip_entries"},
		{RuleZipSize, "zip_size"},
		{RuleZipPath, "zip_path"},
		{RuleZipLayout, "zip_layout"},
		{RuleMediaType, "media_type"},
		{RuleMediaSize, "media_size"},
		{RuleMediaDimensions, "media_dimensions"},
		{RuleInsufficientQuestions, "insufficient_questions"},
		{RuleLockedBySimulation, "locked_by_simulation"},
		{RuleAttemptInProgress, "attempt_in_progress"},
		{RuleAttemptExpired, "attempt_expired"},
		{RuleAttemptClosed, "attempt_closed"},
		{RuleTooManySelected, "too_many_selected"},
		{RuleInvalidLabel, "invalid_label"},
		{RuleAnswerLocked, "answer_locked"},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("rule constant = %q, want %q", tc.got, tc.want)
		}
	}
	// 32 distinct rules: no two constants may collapse into the same string.
	seen := map[string]bool{}
	for _, tc := range cases {
		seen[tc.want] = true
	}
	if len(cases) != 32 || len(seen) != 32 {
		t.Errorf("table has %d entries and %d distinct strings, want 32 and 32", len(cases), len(seen))
	}
}
