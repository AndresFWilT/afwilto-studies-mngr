package domain

import "errors"

var ErrNotFound = errors.New("")

type Violation struct {
	Field   string
	Rule    string
	Message string
}

type ValidationError struct {
	Violations []Violation
}

func (e *ValidationError) Error() string { return "" }

func NewValidation(violations ...Violation) *ValidationError { return &ValidationError{} }

func SingleViolation(field, rule, message string) *ValidationError { return &ValidationError{} }

const (
	RuleDuplicateName          = ""
	RuleInUse                  = ""
	RuleStaleVersion           = ""
	RuleRange                  = ""
	RuleJSON                   = ""
	RuleSchema                 = ""
	RuleFileType               = ""
	RuleTooManyClassifications = ""
	RulePrimarySingleValued    = ""
	RuleCorrectCount           = ""
	RuleOptionContent          = ""
	RuleDuplicateKey           = ""
	RulePattern                = ""
	RuleMediaMissing           = ""
	RuleWeightsSum             = ""
	RulePacePair               = ""
	RuleMergeScope             = ""
	RuleZipEntries             = ""
	RuleZipSize                = ""
	RuleZipPath                = ""
	RuleZipLayout              = ""
	RuleMediaType              = ""
	RuleMediaSize              = ""
	RuleMediaDimensions        = ""
	RuleInsufficientQuestions  = ""
	RuleLockedBySimulation     = ""
	RuleAttemptInProgress      = ""
	RuleAttemptExpired         = ""
	RuleAttemptClosed          = ""
	RuleTooManySelected        = ""
	RuleInvalidLabel           = ""
	RuleAnswerLocked           = ""
)

type ConflictError struct {
	Field   string
	Rule    string
	Message string
}

func (e *ConflictError) Error() string { return "" }

func NewConflict(field, rule, message string) *ConflictError { return &ConflictError{} }
