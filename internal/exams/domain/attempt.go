package domain

import (
	"slices"
	"time"
)

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
func (s ItemSnapshot) ChooseCount() int {
	if s.Type != QuestionMulti {
		return 0
	}
	n := 0
	for _, o := range s.Options {
		if o.Correct {
			n++
		}
	}
	return n
}

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

// OptionLabel is the letter of the option shown at an index: A, B, C…
func OptionLabel(index int) string { return string(rune('A' + index)) }

// StartAttempt validates the config, draws the session and freezes it
// (FR-9): the snapshot of each item copies everything it needs, so a later
// edit or delete of the question never reaches the attempt.
func StartAttempt(id string, exam Exam, bank Bank, cfg SessionConfig, seed uint64, now time.Time) (Attempt, error) {
	if violations := cfg.Violations(); len(violations) > 0 {
		return Attempt{}, NewValidation(violations...)
	}
	draw, err := DrawSession(exam, bank, cfg, seed)
	if err != nil {
		return Attempt{}, err
	}

	attempt := Attempt{
		ID: id, ExamID: exam.ID, Config: cfg, Status: AttemptInProgress, Seed: seed, StartedAt: now,
		SuggestedMinutes: draw.SuggestedMinutes, PassPercent: exam.PassPercent,
		Items: make([]AttemptItem, len(draw.Questions)),
	}
	if minutes, timed := cfg.TimedMinutes(exam); timed {
		deadline := now.Add(time.Duration(minutes) * time.Minute)
		attempt.DeadlineAt = &deadline
	}
	for i, d := range draw.Questions {
		q := d.Question
		snapshot := ItemSnapshot{
			Key: q.Key, Type: q.Type, Stem: q.Stem, Explanation: q.Explanation,
			Categories: slices.Clone(q.Categories), PaceMinutes: d.Pace,
			Options: make([]SnapshotOption, len(d.Options)),
		}
		if q.Origin != nil {
			origin := *q.Origin
			snapshot.Origin = &origin
		}
		for j, o := range d.Options {
			snapshot.Options[j] = SnapshotOption{Label: OptionLabel(j), Text: o.Text, Image: o.Image, Alt: o.Alt, Correct: o.Correct, Rationale: o.Rationale}
		}
		attempt.Items[i] = AttemptItem{Position: i, QuestionID: q.ID, Snapshot: snapshot}
	}
	return attempt, nil
}

// RemainingSeconds is max(0, ceil(deadline - now)); nil when untimed.
func (a Attempt) RemainingSeconds(now time.Time) *int {
	if a.DeadlineAt == nil {
		return nil
	}
	remaining := 0
	if left := a.DeadlineAt.Sub(now); left > 0 {
		remaining = int((left + time.Second - 1) / time.Second)
	}
	return &remaining
}
