package usecase

import (
	"testing"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/exams/testsupport"
)

// sessionEnv adds the slice 06b services to the 06a environment. They share its
// fake clock and attempt repository, so a test moves time once and every
// service sees it.
type sessionEnv struct {
	attemptEnv
	notes   *testsupport.FakeNoteDirectory
	get     in.GetAttempt
	answer  in.AnswerItem
	submit  in.SubmitAttempt
	review  in.ReviewAttempt
	history in.ListExamAttempts
}

func newSessionEnv(t *testing.T) sessionEnv {
	t.Helper()
	e := newAttemptEnv(t)
	notes := testsupport.NewFakeNoteDirectory()
	return sessionEnv{
		attemptEnv: e, notes: notes,
		get:     NewGetAttemptService(e.attempts, e.clock),
		answer:  NewAnswerItemService(e.attempts, e.clock),
		submit:  NewSubmitAttemptService(e.attempts, e.clock),
		review:  NewReviewAttemptService(e.attempts, notes, e.clock),
		history: NewListExamAttemptsService(e.repo, e.attempts),
	}
}

func timedRandom(count, minutes int, feedback domain.Feedback) domain.SessionConfig {
	return domain.SessionConfig{
		Selection: domain.Selection{Kind: domain.SelectRandom, Count: count},
		Timing:    &domain.Timing{Kind: domain.Timed, Minutes: minutes}, Feedback: feedback,
	}
}

func untimedEach(count int) domain.SessionConfig {
	cfg := randomUntimed(count)
	cfg.Feedback = domain.FeedbackEach
	return cfg
}

// keyOf is the stored answer key of an item, read from the repository so a
// test never depends on the shuffle.
func (e sessionEnv) keyOf(id string, position int) []string {
	return e.attempts.Rows[id].Items[position].Snapshot.CorrectLabels()
}

// wrongOf is a label of the item that is not correct.
func (e sessionEnv) wrongOf(id string, position int) string {
	for _, o := range e.attempts.Rows[id].Items[position].Snapshot.Options {
		if !o.Correct {
			return o.Label
		}
	}
	return ""
}

func (e sessionEnv) save(t *testing.T, id string, position int, flagged bool, labels ...string) domain.AnswerOutcome {
	t.Helper()
	if labels == nil {
		labels = []string{}
	}
	out, err := e.answer.Execute(ctx, id, position, domain.AnswerInput{Selected: labels, Flagged: flagged})
	if err != nil {
		t.Fatalf("answer %d %v: %v", position, labels, err)
	}
	return out
}

func (e sessionEnv) read(t *testing.T, id string) domain.AttemptView {
	t.Helper()
	v, err := e.get.Execute(ctx, id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return v
}

func (e sessionEnv) at(d time.Duration) { e.clock.T = t0.Add(d) }
