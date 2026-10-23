package domain

import "time"

// ClassificationRename renames a classification; a nil Name leaves it.
type ClassificationRename struct {
	ID   string
	Name *string
}

// PaceEdit is the pace pair of one category in a PATCH. A side that was not
// sent is not Set; a side sent as null is Set with a nil value (BR-C4).
type PaceEdit struct {
	QuestionsSet bool
	Questions    *int
	MinutesSet   bool
	Minutes      *int
}

// CategoryEdit renames a category and/or sets its pace.
type CategoryEdit struct {
	ID   string
	Name *string
	Pace PaceEdit
}

// StructureEdit is the classification and category part of a PATCH (FR-1).
type StructureEdit struct {
	Classifications []ClassificationRename
	PrimaryID       string // "" when absent
	Weights         []WeightInput
	SetWeights      bool // weights was sent
	InferWeights    bool // weightMode "inferred" was sent
	Categories      []CategoryEdit
}

// IsZero reports an edit that changes nothing.
func (e StructureEdit) IsZero() bool { return false }

// CategoryMerge moves the links of From to Into and deletes From.
type CategoryMerge struct {
	ClassificationID string
	From, Into       string
}

// EditWrite is everything an exam edit, a merge or a question delete persists
// in one transaction. Classifications and Categories hold only the changed
// rows, in full; Weights the weight of each category that changes.
type EditWrite struct {
	ExamID           string
	Now              time.Time
	Exam             *Exam // the exam row after the edit, nil when it is left as it is
	ExpectedVersion  int
	Classifications  []Classification
	Categories       []Category
	Weights          []CategoryWeight
	Merge            *CategoryMerge
	DeleteQuestionID string
}

// Bump increments the version without changing a field (a merge).
func (e *Exam) Bump(now time.Time) {}

// PlanExamEdit applies the exam fields and the structure edit to the loaded
// state, or fails with every violation (422) or one conflict (409).
func PlanExamEdit(exam Exam, bank Bank, expectedVersion int, edit ExamEdit, structure StructureEdit, now time.Time) (EditWrite, error) {
	return EditWrite{}, errNotImplemented
}

// PlanMerge merges category From into Into (BR-C5) and bumps the exam version.
func PlanMerge(exam Exam, bank Bank, from, into string, now time.Time) (EditWrite, error) {
	return EditWrite{}, errNotImplemented
}

// PlanDeleteQuestion deletes a question logically and recomputes the inferred weights.
func PlanDeleteQuestion(exam Exam, bank Bank, questionID string, now time.Time) (EditWrite, error) {
	return EditWrite{}, errNotImplemented
}
