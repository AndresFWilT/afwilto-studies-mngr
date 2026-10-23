package domain

import (
	"errors"
	"strings"
)

// ErrNotFound covers unknown ids and logically deleted rows alike — a deleted
// row is invisible to every operation (Spec 16 FR-0).
var ErrNotFound = errors.New("not found")

// ErrInvalidText is what a bank adapter answers when the database refuses a
// text value it cannot store (a NUL byte); the import turns it into a 422
// report instead of a 500.
var ErrInvalidText = errors.New("text the database cannot store")

type Violation struct {
	Field   string
	Rule    string
	Message string
}

// ValidationError carries field-level violations; the HTTP adapter translates
// it to 422. Both shape and business-rule failures share it.
type ValidationError struct {
	Violations []Violation
}

func (e *ValidationError) Error() string {
	msgs := make([]string, len(e.Violations))
	for i, v := range e.Violations {
		msgs[i] = v.Field + ": " + v.Message
	}
	return "validation failed: " + strings.Join(msgs, "; ")
}

func NewValidation(violations ...Violation) *ValidationError {
	return &ValidationError{Violations: violations}
}

func SingleViolation(field, rule, message string) *ValidationError {
	return NewValidation(Violation{Field: field, Rule: rule, Message: message})
}

// Rules: the wire strings of `details[].rule` (Spec 18 §4 and the closed set
// of §6). The OpenAPI `ErrorDetail.rule` description lists the same set.
const (
	// Reused from Spec 16 §7 (HTTP 409).
	RuleDuplicateName = "duplicate_name"
	RuleInUse         = "in_use"
	RuleStaleVersion  = "stale_version"

	// Slice 02.
	RuleRange = "range"

	// Slice 03: import.
	RuleJSON                   = "json"
	RuleSchema                 = "schema"
	RuleFileType               = "file_type"
	RuleTooManyClassifications = "too_many_classifications"
	RulePrimarySingleValued    = "primary_single_valued"
	RuleCorrectCount           = "correct_count"
	RuleOptionContent          = "option_content"
	RuleDuplicateKey           = "duplicate_key"
	RulePattern                = "pattern"
	RuleMediaMissing           = "media_missing"
	RuleNotFound               = "not_found"
	RuleInvalidText            = "invalid_text"

	// Slice 04: bank and category editing.
	RuleWeightsSum = "weights_sum"
	RulePacePair   = "pace_pair"
	RuleMergeScope = "merge_scope"

	// Slice 05: zip and media.
	RuleZipEntries      = "zip_entries"
	RuleZipSize         = "zip_size"
	RuleZipPath         = "zip_path"
	RuleZipLayout       = "zip_layout"
	RuleMediaType       = "media_type"
	RuleMediaSize       = "media_size"
	RuleMediaDimensions = "media_dimensions"

	// Slice 06a: sessions and start.
	RuleInsufficientQuestions = "insufficient_questions"
	RuleLockedBySimulation    = "locked_by_simulation"
	RuleAttemptInProgress     = "attempt_in_progress"

	// Slice 06b: answering and closing.
	RuleAttemptExpired  = "attempt_expired"
	RuleAttemptClosed   = "attempt_closed"
	RuleTooManySelected = "too_many_selected"
	RuleInvalidLabel    = "invalid_label"
	RuleAnswerLocked    = "answer_locked"
)

// ConflictError is a business conflict with the current state: a duplicate
// name, a delete blocked by alive dependents, a stale version or an attempt
// state clash.
type ConflictError struct {
	Field   string
	Rule    string
	Message string
}

func (e *ConflictError) Error() string { return "conflict: " + e.Rule + ": " + e.Message }

func NewConflict(field, rule, message string) *ConflictError {
	return &ConflictError{Field: field, Rule: rule, Message: message}
}
