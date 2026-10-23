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
