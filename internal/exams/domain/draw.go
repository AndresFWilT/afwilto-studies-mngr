package domain

// DrawnQuestion is a drawn question with its options in displayed order.
type DrawnQuestion struct {
	Question Question
	Options  []Option
	Pace     float64
}

// SessionDraw is the questions of an attempt in presentation order.
type SessionDraw struct {
	Questions        []DrawnQuestion
	SuggestedMinutes int
}

// DrawSession selects, orders and prices the questions of a session with the
// seeded generator (FR-9). A *ValidationError with Shortage details when the
// bank cannot satisfy it.
func DrawSession(exam Exam, bank Bank, cfg SessionConfig, seed uint64) (SessionDraw, error) {
	return SessionDraw{}, errNotImplemented
}
