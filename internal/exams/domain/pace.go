package domain

import "math"

// examPace is the minutes per question of the real simulation.
func examPace(exam Exam) float64 { return float64(exam.TimeLimitMin) / float64(exam.QuestionCount) }

// categoryPace is paceMinutes / paceQuestions, or the exam pace without one.
func categoryPace(exam Exam, c Category) float64 {
	if c.PaceQuestions != nil && c.PaceMinutes != nil {
		return float64(*c.PaceMinutes) / float64(*c.PaceQuestions)
	}
	return examPace(exam)
}

// QuestionPace is the minutes a question is priced at (FR-8): the largest
// pace among its categories in the classification used for selection, or the
// exam pace when it has none there (or no classification applies). A category
// the classification no longer lists counts at the exam pace.
func QuestionPace(exam Exam, classification *Classification, q Question) float64 {
	pace := examPace(exam)
	if classification == nil {
		return pace
	}
	class := Fold(classification.Name)
	best, found := 0.0, false
	for _, ref := range q.Categories {
		if Fold(ref.Classification) != class {
			continue
		}
		candidate := pace
		for _, c := range classification.Categories {
			if Fold(c.Name) == Fold(ref.Category) {
				candidate = categoryPace(exam, c)
			}
		}
		if !found || candidate > best {
			best, found = candidate, true
		}
	}
	if !found {
		return pace
	}
	return best
}

// paceNoise is far above the error of summing up to 200 float paces and far
// below the gap between a sum and the next whole minute.
const paceNoise = 1e-9

// CeilMinutes rounds a sum of paces up to whole minutes. The noise term keeps
// 6 × (7/3), which floats sum to 14.000000000000002, at 14 and not 15.
func CeilMinutes(minutes float64) int {
	return max(0, int(math.Ceil(minutes-paceNoise)))
}
