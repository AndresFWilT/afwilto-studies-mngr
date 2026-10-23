package domain

import "time"

// AttemptStatus is the lifecycle of an attempt (Spec 18 §2).
type AttemptStatus string

const (
	AttemptInProgress AttemptStatus = "in_progress"
	AttemptSubmitted  AttemptStatus = "submitted"
	AttemptExpired    AttemptStatus = "expired"
)

// SnapshotOption is one option as displayed in an attempt: relabelled A, B…
// in the shuffled order, with its correct flag and rationale.
type SnapshotOption struct {
	Label     string
	Text      string
	Image     string
	Alt       string
	Correct   bool
	Rationale string
}

// ItemSnapshot freezes a question as it was when the attempt started.
type ItemSnapshot struct {
	Key         string
	Type        QuestionType
	Stem        string
	Options     []SnapshotOption
	Explanation string
	Categories  []CategoryRef
	Origin      *Origin
	PaceMinutes float64
}

// ChooseCount is N of a multi question (its number of correct options), 0 for
// single.
func (s ItemSnapshot) ChooseCount() int { return 0 }

// AttemptItem is one question of an attempt. 06b fills in the answer state.
type AttemptItem struct {
	Position   int
	QuestionID string
	Snapshot   ItemSnapshot
	Selected   []string
	Flagged    bool
	AnsweredAt *time.Time
	Revealed   bool
	Correct    *bool
}

// Attempt is one run through a drawn session (Spec 18 §2). SuggestedMinutes
// and PassPercent are frozen at the start.
type Attempt struct {
	ID               string
	ExamID           string
	Config           SessionConfig
	Status           AttemptStatus
	Seed             uint64
	StartedAt        time.Time
	DeadlineAt       *time.Time
	SubmittedAt      *time.Time
	ScorePercent     *float64
	Passed           *bool
	SuggestedMinutes int
	PassPercent      int
	Items            []AttemptItem
}

// AttemptView is an attempt plus the server-computed remaining time.
type AttemptView struct {
	Attempt
	RemainingSeconds *int
}

// StartAttempt validates the config, draws the session and freezes it.
func StartAttempt(id string, exam Exam, bank Bank, cfg SessionConfig, seed uint64, now time.Time) (Attempt, error) {
	return Attempt{}, errNotImplemented
}

// RemainingSeconds is max(0, ceil(deadline - now)); nil when untimed.
func (a Attempt) RemainingSeconds(now time.Time) *int { return nil }
