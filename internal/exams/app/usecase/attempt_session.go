package usecase

import (
	"context"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// Lazy expiry (FR-10, BR-A2): no job closes a late attempt. Every operation on
// an attempt checks, through the Clock port, whether it is past deadlineAt + 5 s
// and, if so, scores it as it stands and sets it to expired before the request
// continues.

// expiryChange stores an expired attempt: its header and every scored item.
func expiryChange(expired domain.Attempt) *out.AttemptChange {
	return &out.AttemptChange{Header: &expired, Items: expired.Items}
}

// finalizeIfDue expires the attempt when it is past its grace period and
// returns its current state and whether this call expired it. The check runs
// again under the row lock, so two requests finalize it once.
func finalizeIfDue(ctx context.Context, attempts out.AttemptRepository, clock out.Clock, id string) (current domain.Attempt, finalized bool, err error) {
	err = attempts.Mutate(ctx, id, func(a domain.Attempt) (*out.AttemptChange, error) {
		current = a
		if !a.PastDeadline(clock.Now()) {
			return nil, nil
		}
		current, finalized = a.Expire(), true
		return expiryChange(current), nil
	})
	return current, finalized, err
}

// loadCurrent reads an attempt and finalizes it first when it is past its
// deadline. Reads of an attempt that is not due take no lock.
func loadCurrent(ctx context.Context, attempts out.AttemptRepository, clock out.Clock, id string) (domain.Attempt, error) {
	a, err := attempts.Get(ctx, id)
	if err != nil {
		return domain.Attempt{}, err
	}
	if a.PastDeadline(clock.Now()) {
		a, _, err = finalizeIfDue(ctx, attempts, clock, id)
	}
	return a, err
}

type getAttemptService struct {
	attempts out.AttemptRepository
	clock    out.Clock
}

func NewGetAttemptService(attempts out.AttemptRepository, clock out.Clock) in.GetAttempt {
	return &getAttemptService{attempts: attempts, clock: clock}
}

func (s *getAttemptService) Execute(ctx context.Context, id string) (domain.AttemptView, error) {
	a, err := loadCurrent(ctx, s.attempts, s.clock, id)
	if err != nil {
		return domain.AttemptView{}, err
	}
	return viewOf(a, s.clock.Now()), nil
}

// viewOf adds the server clock and, for a closed attempt, its result. A closed
// timed attempt has no time left.
func viewOf(a domain.Attempt, now time.Time) domain.AttemptView {
	v := domain.AttemptView{Attempt: a}
	if a.Status == domain.AttemptInProgress {
		v.RemainingSeconds = a.RemainingSeconds(now)
		return v
	}
	if a.DeadlineAt != nil {
		none := 0
		v.RemainingSeconds = &none
	}
	result := a.Result()
	v.Result = &result
	return v
}

type answerItemService struct {
	attempts out.AttemptRepository
	clock    out.Clock
}

func NewAnswerItemService(attempts out.AttemptRepository, clock out.Clock) in.AnswerItem {
	return &answerItemService{attempts: attempts, clock: clock}
}

func (s *answerItemService) Execute(ctx context.Context, id string, position int, answer domain.AnswerInput) (domain.AnswerOutcome, error) {
	var outcome domain.AnswerOutcome
	err := s.attempts.Mutate(ctx, id, func(a domain.Attempt) (*out.AttemptChange, error) {
		now := s.clock.Now()
		if a.PastDeadline(now) { // BR-A2: persist the expiry, then refuse the write
			return expiryChange(a.Expire()), domain.NewAttemptExpired()
		}
		next, result, err := a.Answer(position, answer, now)
		if err != nil {
			return nil, err
		}
		result.RemainingSeconds = next.RemainingSeconds(now)
		outcome = result
		return &out.AttemptChange{Items: []domain.AttemptItem{next.Items[position]}}, nil
	})
	if err != nil {
		return domain.AnswerOutcome{}, err
	}
	return outcome, nil
}

type submitAttemptService struct {
	attempts out.AttemptRepository
	clock    out.Clock
}

func NewSubmitAttemptService(attempts out.AttemptRepository, clock out.Clock) in.SubmitAttempt {
	return &submitAttemptService{attempts: attempts, clock: clock}
}

func (s *submitAttemptService) Execute(ctx context.Context, id string) (domain.AttemptResult, error) {
	var result domain.AttemptResult
	err := s.attempts.Mutate(ctx, id, func(a domain.Attempt) (*out.AttemptChange, error) {
		now := s.clock.Now()
		if a.PastDeadline(now) { // S06b-8: scored by this call, but the submit came too late
			return expiryChange(a.Expire()), domain.NewAttemptClosed()
		}
		if a.Status != domain.AttemptInProgress {
			return nil, domain.NewAttemptClosed()
		}
		closed := a.Submit(now)
		result = closed.Result()
		return &out.AttemptChange{Header: &closed, Items: closed.Items}, nil
	})
	if err != nil {
		return domain.AttemptResult{}, err
	}
	return result, nil
}

type reviewAttemptService struct {
	attempts out.AttemptRepository
	notes    out.NoteDirectory
	clock    out.Clock
}

func NewReviewAttemptService(attempts out.AttemptRepository, notes out.NoteDirectory, clock out.Clock) in.ReviewAttempt {
	return &reviewAttemptService{attempts: attempts, notes: notes, clock: clock}
}

func (s *reviewAttemptService) Execute(ctx context.Context, id string) ([]domain.ReviewItem, error) {
	a, err := loadCurrent(ctx, s.attempts, s.clock, id)
	if err != nil {
		return nil, err
	}
	if a.Status == domain.AttemptInProgress {
		return nil, domain.NewAttemptInProgress(a.ID)
	}

	titles := map[string]bool{}
	for _, it := range a.Items {
		if it.Snapshot.Origin != nil {
			if title := domain.NoteTitleFromRef(it.Snapshot.Origin.NoteRef); title != "" {
				titles[title] = true
			}
		}
	}
	noteIDs := map[string]string{}
	if len(titles) > 0 {
		list := make([]string, 0, len(titles))
		for title := range titles {
			list = append(list, title)
		}
		if noteIDs, err = s.notes.FindNoteIDs(ctx, a.ExamID, list); err != nil {
			return nil, err
		}
	}

	items := make([]domain.ReviewItem, len(a.Items))
	for i, it := range a.Items {
		items[i] = domain.ReviewItem{Item: it, Reveal: it.Reveal()}
		if it.Snapshot.Origin != nil {
			items[i].NoteID = noteIDs[domain.NoteTitleFromRef(it.Snapshot.Origin.NoteRef)]
		}
	}
	return items, nil
}

type listExamAttemptsService struct {
	exams    out.ExamRepository
	attempts out.AttemptRepository
}

func NewListExamAttemptsService(exams out.ExamRepository, attempts out.AttemptRepository) in.ListExamAttempts {
	return &listExamAttemptsService{exams: exams, attempts: attempts}
}

// Execute is a pure read: it does not finalize an open attempt.
func (s *listExamAttemptsService) Execute(ctx context.Context, examID string) (domain.AttemptHistory, error) {
	if _, err := s.exams.Get(ctx, examID); err != nil {
		return domain.AttemptHistory{}, err
	}
	rows, err := s.attempts.ListByExam(ctx, examID)
	if err != nil {
		return domain.AttemptHistory{}, err
	}
	return domain.NewHistory(rows), nil
}
