package domain

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode"
)

const (
	maxClassificationNameRunes = 40
	maxCategoryNameRunes       = 80
	maxOffenderKeys            = 10
)

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
func (e StructureEdit) IsZero() bool {
	return len(e.Classifications) == 0 && e.PrimaryID == "" && !e.SetWeights && !e.InferWeights && len(e.Categories) == 0
}

// CategoryMerge moves the links of From to Into and deletes From.
type CategoryMerge struct {
	ClassificationID string
	From, Into       string
}

// EditWrite is everything an exam edit, a merge or a question delete persists
// in one transaction. Classifications and Categories hold only the changed
// rows, in full (a classification without its categories); Weights the
// weight of each category that changes. Classifications are ordered so that a
// demotion comes before the promotion that replaces it.
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
func (e *Exam) Bump(now time.Time) {
	e.Version++
	e.UpdatedAt = now
}

func staleVersion() *ConflictError {
	return NewConflict("version", RuleStaleVersion, "the exam was changed since you read it; reload and re-apply your edit")
}

// cleanName trims a name and checks its length in runes and its characters.
func cleanName(field, raw string, maxRunes int) (string, *Violation) {
	name := strings.TrimSpace(raw)
	switch {
	case name == "":
		return "", &Violation{Field: field, Rule: "required", Message: "name must not be empty"}
	case strings.ContainsFunc(name, unicode.IsControl):
		return "", &Violation{Field: field, Rule: "format", Message: "name must not contain control characters"}
	case len([]rune(name)) > maxRunes:
		return "", &Violation{Field: field, Rule: "max_length", Message: fmt.Sprintf("name must be at most %d characters", maxRunes)}
	}
	return name, nil
}

// ── PlanExamEdit ───────────────────────────────────────────────────────────

// renameEntry is one rename of the request, for the duplicate-name check.
type renameEntry struct {
	id, scope, fold, field string
}

// PlanExamEdit applies the exam fields and the structure edit to the loaded
// state (FR-1, BR-C2 to BR-C4). It fails with a stale_version conflict, with
// every violation together (422), or with one duplicate_name conflict (409),
// in that order; otherwise it returns the one transaction to persist.
func PlanExamEdit(exam Exam, bank Bank, expectedVersion int, edit ExamEdit, st StructureEdit, now time.Time) (EditWrite, error) {
	if exam.Version != expectedVersion {
		return EditWrite{}, staleVersion()
	}
	updated := exam
	var violations []Violation
	if err := updated.Edit(edit, now); err != nil {
		var v *ValidationError
		if errors.As(err, &v) {
			violations = append(violations, v.Violations...)
		}
	}

	classes := make([]Classification, len(bank.Classifications)) // working copy
	for i, c := range bank.Classifications {
		c.Categories = slices.Clone(c.Categories)
		classes[i] = c
	}
	changedClass := map[string]bool{}
	changedCat := map[string]bool{}
	findClass := func(id string) *Classification {
		for i := range classes {
			if classes[i].ID == id {
				return &classes[i]
			}
		}
		return nil
	}
	findCat := func(id string) (*Category, *Classification) {
		for i := range classes {
			for j := range classes[i].Categories {
				if classes[i].Categories[j].ID == id {
					return &classes[i].Categories[j], &classes[i]
				}
			}
		}
		return nil, nil
	}

	var renames []renameEntry
	renamed := map[string]bool{}
	for i, r := range st.Classifications {
		c := findClass(r.ID)
		if c == nil {
			violations = append(violations, Violation{Field: fmt.Sprintf("classifications[%d].id", i), Rule: RuleNotFound, Message: "classification does not exist"})
			continue
		}
		if r.Name == nil {
			continue
		}
		field := fmt.Sprintf("classifications[%d].name", i)
		name, bad := cleanName(field, *r.Name, maxClassificationNameRunes)
		if bad != nil {
			violations = append(violations, *bad)
			continue
		}
		c.Name, c.Fold = name, Fold(name)
		changedClass[c.ID] = true
		renamed[c.ID] = true
		renames = append(renames, renameEntry{id: c.ID, scope: "classifications", fold: c.Fold, field: field})
	}
	for i, e := range st.Categories {
		k, owner := findCat(e.ID)
		if k == nil {
			violations = append(violations, Violation{Field: fmt.Sprintf("categories[%d].id", i), Rule: RuleNotFound, Message: "category does not exist"})
			continue
		}
		if e.Name != nil {
			field := fmt.Sprintf("categories[%d].name", i)
			if name, bad := cleanName(field, *e.Name, maxCategoryNameRunes); bad != nil {
				violations = append(violations, *bad)
			} else {
				k.Name, k.Key, k.Fold = name, Slug(name), Fold(name)
				changedCat[k.ID] = true
				renamed[k.ID] = true
				renames = append(renames, renameEntry{id: k.ID, scope: "classification:" + owner.ID, fold: k.Fold, field: field})
			}
		}
		if e.Pace.QuestionsSet || e.Pace.MinutesSet {
			if v := applyPace(k, e.Pace, fmt.Sprintf("categories[%d].pace", i)); v != nil {
				violations = append(violations, *v)
			} else {
				changedCat[k.ID] = true
			}
		}
	}

	// The primary: the one named, or the current one.
	currentIdx := -1
	for i := range classes {
		if classes[i].IsPrimary() {
			currentIdx = i
		}
	}
	var oldPrimary *Classification
	if currentIdx >= 0 {
		oldPrimary = &classes[currentIdx]
	}
	newPrimary := oldPrimary
	swap := false
	if st.PrimaryID != "" {
		target := findClass(st.PrimaryID)
		switch {
		case target == nil:
			violations = append(violations, Violation{Field: "primaryClassificationId", Rule: RuleNotFound, Message: "classification does not exist"})
		case target != oldPrimary:
			if v := primarySingleValued(bank, target.ID, target.Name); v != nil {
				violations = append(violations, *v)
			} else {
				swap, newPrimary = true, target
			}
		}
	}

	if st.SetWeights && st.InferWeights {
		violations = append(violations, Violation{Field: "weightMode", Rule: "format", Message: "weightMode inferred cannot be combined with weights"})
	}
	if st.SetWeights {
		if newPrimary == nil {
			violations = append(violations, Violation{Field: "weights", Rule: RuleWeightsSum, Message: "the exam has no primary classification to weigh"})
		} else {
			violations = append(violations, ValidateManualWeights(newPrimary.Categories, st.Weights)...)
		}
	}
	if len(violations) > 0 {
		return EditWrite{}, NewValidation(violations...)
	}

	if err := checkRenames(classes, renamed, renames); err != nil {
		return EditWrite{}, err
	}

	// Positions, mode and weights of the primary.
	var weights []CategoryWeight
	if swap {
		if oldPrimary != nil {
			oldPrimary.Position, oldPrimary.WeightMode = PositionSecondary, WeightInferred
			changedClass[oldPrimary.ID] = true
			for _, k := range oldPrimary.Categories {
				weights = append(weights, CategoryWeight{CategoryID: k.ID, Percent: 0})
			}
		}
		newPrimary.Position, newPrimary.WeightMode = PositionPrimary, WeightInferred
		changedClass[newPrimary.ID] = true
	}
	if newPrimary != nil {
		mode := newPrimary.WeightMode
		switch {
		case st.SetWeights:
			mode = WeightManual
		case st.InferWeights:
			mode = WeightInferred
		}
		if mode != newPrimary.WeightMode {
			newPrimary.WeightMode = mode
			changedClass[newPrimary.ID] = true
		}
		switch {
		case st.SetWeights:
			byID := map[string]int{}
			for _, w := range st.Weights {
				byID[w.CategoryID] = w.Percent
			}
			for _, k := range newPrimary.Categories {
				weights = append(weights, CategoryWeight{CategoryID: k.ID, Percent: byID[k.ID]})
			}
		case mode == WeightInferred && (swap || st.InferWeights):
			weights = append(weights, InferWeights(newPrimary.Categories, bank.LinkCounts())...)
		}
	}

	write := EditWrite{ExamID: exam.ID, Now: now, Exam: &updated, ExpectedVersion: expectedVersion, Weights: weights}
	for _, c := range classes {
		for _, k := range c.Categories {
			if changedCat[k.ID] {
				write.Categories = append(write.Categories, k)
			}
		}
		if changedClass[c.ID] {
			c.Categories = nil
			write.Classifications = append(write.Classifications, c)
		}
	}
	slices.SortStableFunc(write.Classifications, func(a, b Classification) int { return b.Position - a.Position })
	return write, nil
}

// applyPace sets the pace pair of k, or reports pace_pair (BR-C4): both sides
// sent, as two integers >= 1 or as two nulls.
func applyPace(k *Category, p PaceEdit, field string) *Violation {
	bad := func(msg string) *Violation { return &Violation{Field: field, Rule: RulePacePair, Message: msg} }
	switch {
	case p.QuestionsSet != p.MinutesSet:
		return bad("paceQuestions and paceMinutes are set together: send both, or both as null")
	case (p.Questions == nil) != (p.Minutes == nil):
		return bad("paceQuestions and paceMinutes are both numbers or both null")
	case p.Questions != nil && (*p.Questions < 1 || *p.Minutes < 1):
		return bad("paceQuestions and paceMinutes must each be at least 1")
	}
	k.PaceQuestions, k.PaceMinutes = p.Questions, p.Minutes
	return nil
}

// primarySingleValued is BR-C2 for the classification that would become
// primary: no alive question may have two categories in it.
func primarySingleValued(bank Bank, classificationID, name string) *Violation {
	idx := bank.refIndex()
	var offenders []string
	for _, q := range bank.Questions {
		n := 0
		for _, id := range idx.categoryIDs(q) {
			for _, hit := range idx {
				if hit.categoryID == id && hit.classificationID == classificationID {
					n++
					break
				}
			}
		}
		if n > 1 {
			offenders = append(offenders, q.Key)
		}
	}
	if len(offenders) == 0 {
		return nil
	}
	listed := offenders
	if len(listed) > maxOffenderKeys {
		listed = listed[:maxOffenderKeys]
	}
	msg := fmt.Sprintf("%d question(s) have more than one category in %q: %s", len(offenders), name, strings.Join(listed, ", "))
	if len(offenders) > len(listed) {
		msg += fmt.Sprintf(" (and %d more)", len(offenders)-len(listed))
	}
	return &Violation{Field: "primaryClassificationId", Rule: RulePrimarySingleValued, Message: msg}
}

// checkRenames reports the first rename whose folded name is taken in its scope
// by an item that keeps its name or was renamed earlier in the request. Names
// compare after the whole request is applied, so two items can swap names.
func checkRenames(classes []Classification, renamed map[string]bool, renames []renameEntry) error {
	taken := map[string]string{}
	for _, c := range classes {
		if !renamed[c.ID] {
			taken["classifications\x00"+c.Fold] = c.ID
		}
		for _, k := range c.Categories {
			if !renamed[k.ID] {
				taken["classification:"+c.ID+"\x00"+k.Fold] = k.ID
			}
		}
	}
	for _, r := range renames {
		key := r.scope + "\x00" + r.fold
		if owner, clash := taken[key]; clash && owner != r.id {
			return NewConflict(r.field, RuleDuplicateName, "another one with this name already exists")
		}
		taken[key] = r.id
	}
	return nil
}

// ── PlanMerge ──────────────────────────────────────────────────────────────

// PlanMerge merges category From into Into (FR-1, BR-C5): both must be alive
// categories of one classification. The exam version increments. On the
// primary, manual weights add up in Into and inferred ones are recomputed.
func PlanMerge(exam Exam, bank Bank, from, into string, now time.Time) (EditWrite, error) {
	var fromClass, intoClass *Classification
	var fromCat, intoCat *Category
	for i := range bank.Classifications {
		c := &bank.Classifications[i]
		for j := range c.Categories {
			switch c.Categories[j].ID {
			case from:
				fromClass, fromCat = c, &c.Categories[j]
			case into:
				intoClass, intoCat = c, &c.Categories[j]
			}
		}
	}
	scope := func(field, msg string) error {
		return SingleViolation(field, RuleMergeScope, msg)
	}
	switch {
	case fromCat == nil:
		return EditWrite{}, scope("from", "from must be an alive category of the exam")
	case intoCat == nil:
		return EditWrite{}, scope("into", "into must be an alive category of the exam")
	case from == into:
		return EditWrite{}, scope("into", "a category cannot be merged into itself")
	case fromClass.ID != intoClass.ID:
		return EditWrite{}, scope("into", "both categories must belong to the same classification")
	}

	updated := exam
	updated.Bump(now)
	write := EditWrite{
		ExamID: exam.ID, Now: now, Exam: &updated, ExpectedVersion: exam.Version,
		Merge: &CategoryMerge{ClassificationID: fromClass.ID, From: from, Into: into},
	}
	if fromClass.IsPrimary() {
		if fromClass.WeightMode == WeightManual {
			write.Weights = []CategoryWeight{{CategoryID: into, Percent: intoCat.WeightPercent + fromCat.WeightPercent}}
		} else {
			var remaining []Category
			for _, k := range fromClass.Categories {
				if k.ID != from {
					remaining = append(remaining, k)
				}
			}
			write.Weights = InferWeights(remaining, bank.countLinks("", from, into))
		}
	}
	return write, nil
}

// ── PlanDeleteQuestion ─────────────────────────────────────────────────────

// PlanDeleteQuestion deletes a question logically and, when the primary is in
// inferred mode, recomputes its weights without the question. The exam row
// and its version are left alone, as with an import.
func PlanDeleteQuestion(exam Exam, bank Bank, questionID string, now time.Time) (EditWrite, error) {
	if !slices.ContainsFunc(bank.Questions, func(q Question) bool { return q.ID == questionID }) {
		return EditWrite{}, ErrNotFound
	}
	write := EditWrite{ExamID: exam.ID, Now: now, DeleteQuestionID: questionID}
	if p := bank.primary(); p >= 0 && bank.Classifications[p].WeightMode == WeightInferred {
		write.Weights = InferWeights(bank.Classifications[p].Categories, bank.countLinks(questionID, "", ""))
	}
	return write, nil
}
