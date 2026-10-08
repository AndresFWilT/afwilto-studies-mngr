package domain

import (
	"errors"
	"strings"
)

// ErrNotFound covers unknown ids and logically deleted rows alike — a deleted
// row is invisible to every operation (Spec 16 FR-0).
var ErrNotFound = errors.New("not found")

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

// Conflict rules (Spec 16 §7, HTTP 409).
const (
	RuleDuplicateName = "duplicate_name"
	RuleInUse         = "in_use"
	RuleStaleVersion  = "stale_version"
)

// ConflictError is a business conflict with the current state: a duplicate
// name, a delete blocked by alive dependents, or a stale version.
type ConflictError struct {
	Field   string
	Rule    string
	Message string
}

func (e *ConflictError) Error() string { return "conflict: " + e.Rule + ": " + e.Message }

func NewConflict(field, rule, message string) *ConflictError {
	return &ConflictError{Field: field, Rule: rule, Message: message}
}
