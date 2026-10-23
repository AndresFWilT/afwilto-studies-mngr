package domain

// QuestionPace is the minutes a question is priced at (FR-8): the largest
// pace among its categories in the classification used for selection, or the
// exam pace when it has none there.
func QuestionPace(exam Exam, classification *Classification, q Question) float64 { return 0 }

// CeilMinutes rounds a sum of paces up to whole minutes, absorbing float noise.
func CeilMinutes(minutes float64) int { return 0 }
