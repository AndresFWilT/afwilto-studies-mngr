package usecase

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

type previewSessionService struct {
	exams out.ExamRepository
	bank  out.QuestionBank
}

func NewPreviewSessionService(exams out.ExamRepository, bank out.QuestionBank) in.PreviewSession {
	return &previewSessionService{exams: exams, bank: bank}
}

func (s *previewSessionService) Execute(ctx context.Context, examID string, cfg domain.SessionConfig) (domain.SessionPreview, error) {
	exam, err := s.exams.Get(ctx, examID)
	if err != nil {
		return domain.SessionPreview{}, err
	}
	if violations := cfg.Violations(); len(violations) > 0 {
		return domain.SessionPreview{}, domain.NewValidation(violations...)
	}
	bank, err := s.bank.LoadBank(ctx, examID)
	if err != nil {
		return domain.SessionPreview{}, err
	}
	return domain.PreviewSession(exam, bank, cfg)
}

type startAttemptService struct {
	exams    out.ExamRepository
	bank     out.QuestionBank
	attempts out.AttemptRepository
	clock    out.Clock
	ids      out.IDGenerator
	seeds    out.SeedSource
}

func NewStartAttemptService(exams out.ExamRepository, bank out.QuestionBank, attempts out.AttemptRepository, clock out.Clock, ids out.IDGenerator, seeds out.SeedSource) in.StartAttempt {
	return &startAttemptService{exams: exams, bank: bank, attempts: attempts, clock: clock, ids: ids, seeds: seeds}
}

// Execute orders its checks from cheapest to dearest: the exam, the config,
// the open attempt (BR-A1), then the bank and the draw. The partial unique
// index behind Create stays the guarantee when two starts race.
func (s *startAttemptService) Execute(ctx context.Context, examID string, cmd in.StartAttemptCommand) (domain.AttemptView, error) {
	exam, err := s.exams.Get(ctx, examID)
	if err != nil {
		return domain.AttemptView{}, err
	}
	if violations := cmd.Config.Violations(); len(violations) > 0 {
		return domain.AttemptView{}, domain.NewValidation(violations...)
	}
	if open, found, err := s.attempts.FindInProgress(ctx, examID); err != nil {
		return domain.AttemptView{}, err
	} else if found {
		// An open attempt past deadlineAt + 5 s is finalized now, scored as it
		// stands, and no longer blocks the start (slice 06b decision 3). The
		// status is read under the attempt's row lock, so one that a racing
		// submit or finalize closed since FindInProgress does not block either;
		// a start that still loses the unique index gets the 409 from Create,
		// naming the attempt that is really in progress.
		if current, _, err := finalizeIfDue(ctx, s.attempts, s.clock, open); err != nil {
			return domain.AttemptView{}, err
		} else if current.Status == domain.AttemptInProgress {
			return domain.AttemptView{}, domain.NewAttemptInProgress(open)
		}
	}
	bank, err := s.bank.LoadBank(ctx, examID)
	if err != nil {
		return domain.AttemptView{}, err
	}

	seed := s.seeds.NewSeed()
	if cmd.Seed != nil {
		seed = *cmd.Seed
	}
	now := s.clock.Now()
	attempt, err := domain.StartAttempt(s.ids.NewID(), exam, bank, cmd.Config, seed, now)
	if err != nil {
		return domain.AttemptView{}, err
	}
	if err := s.attempts.Create(ctx, attempt); err != nil {
		return domain.AttemptView{}, err
	}
	return domain.AttemptView{Attempt: attempt, RemainingSeconds: attempt.RemainingSeconds(now)}, nil
}
