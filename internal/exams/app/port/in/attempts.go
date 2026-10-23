package in

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// PreviewSession is the pure read behind the session form: pool size, expected
// draw, suggested minutes and shortages (Spec 18 FR-7, FR-8). A config that
// breaks a rule is a validation error; domain.ErrNotFound when the exam is not
// alive.
type PreviewSession interface {
	Execute(ctx context.Context, examID string, cfg domain.SessionConfig) (domain.SessionPreview, error)
}

// StartAttemptCommand: Seed nil means "pick one"; an explicit 0 is a seed.
type StartAttemptCommand struct {
	Config domain.SessionConfig
	Seed   *uint64
}

// StartAttempt draws and freezes an attempt and opens it (FR-9). Errors: a
// validation error for a bad config or insufficient_questions, a conflict
// attempt_in_progress (BR-A1), domain.ErrNotFound for a dead exam.
type StartAttempt interface {
	Execute(ctx context.Context, examID string, cmd StartAttemptCommand) (domain.AttemptView, error)
}

// GetAttempt reads an attempt after lazily finalizing an expired one (FR-10):
// the start shape plus each item's answer state, the server's remaining time,
// and the result once the attempt is closed. domain.ErrNotFound for an unknown
// attempt.
type GetAttempt interface {
	Execute(ctx context.Context, attemptID string) (domain.AttemptView, error)
}

// AnswerItem saves the selection and flag of one item. Errors: ErrNotFound
// (attempt or position), a validation error invalid_label / too_many_selected,
// a conflict attempt_closed, attempt_expired (this call found the attempt past
// its deadline and finalized it) or answer_locked.
type AnswerItem interface {
	Execute(ctx context.Context, attemptID string, position int, answer domain.AnswerInput) (domain.AnswerOutcome, error)
}

// SubmitAttempt scores and closes an attempt. A second submit, or one after
// the attempt expired, is a conflict attempt_closed.
type SubmitAttempt interface {
	Execute(ctx context.Context, attemptID string) (domain.AttemptResult, error)
}

// ReviewAttempt returns every item of a closed attempt with its key. An open
// attempt is a conflict attempt_in_progress.
type ReviewAttempt interface {
	Execute(ctx context.Context, attemptID string) ([]domain.ReviewItem, error)
}

// ListExamAttempts is the history of an exam; domain.ErrNotFound when the exam
// is not alive.
type ListExamAttempts interface {
	Execute(ctx context.Context, examID string) (domain.AttemptHistory, error)
}
