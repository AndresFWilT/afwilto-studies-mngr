package usecase

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// requireAliveSubject is the friendly pre-check of S02-4; a missing or deleted
// subject is a 422 on subjectId, not a 404 on the exam. The foreign key and the
// repository's alive-subject insert remain the guarantee.
func requireAliveSubject(ctx context.Context, subjects out.SubjectDirectory, subjectID string) error {
	alive, err := subjects.IsAlive(ctx, subjectID)
	if err != nil {
		return err
	}
	if !alive {
		return domain.SingleViolation("subjectId", "not_found", "subject does not exist")
	}
	return nil
}

// ── Create ──────────────────────────────────────────────────────────────────

type createExamService struct {
	repo     out.ExamRepository
	subjects out.SubjectDirectory
	clock    out.Clock
	ids      out.IDGenerator
}

func NewCreateExamService(repo out.ExamRepository, subjects out.SubjectDirectory, clock out.Clock, ids out.IDGenerator) in.CreateExam {
	return &createExamService{repo: repo, subjects: subjects, clock: clock, ids: ids}
}

func (s *createExamService) Execute(ctx context.Context, cmd in.CreateExamCommand) (domain.ExamSummary, error) {
	exam, err := domain.NewExam(s.ids.NewID(), cmd.SubjectID, cmd.Name, cmd.QuestionCount, cmd.TimeLimitMin, cmd.PassPercent, s.clock.Now())
	if err != nil {
		return domain.ExamSummary{}, err
	}
	if err := requireAliveSubject(ctx, s.subjects, cmd.SubjectID); err != nil {
		return domain.ExamSummary{}, err
	}
	if err := s.repo.Create(ctx, exam); err != nil {
		return domain.ExamSummary{}, err
	}
	return s.repo.GetSummary(ctx, exam.ID)
}

// ── Get / List ──────────────────────────────────────────────────────────────

type getExamService struct{ repo out.ExamRepository }

func NewGetExamService(repo out.ExamRepository) in.GetExam { return &getExamService{repo: repo} }

func (s *getExamService) Execute(ctx context.Context, id string) (domain.ExamSummary, error) {
	return s.repo.GetSummary(ctx, id)
}

type listSubjectExamsService struct {
	repo     out.ExamRepository
	subjects out.SubjectDirectory
}

func NewListSubjectExamsService(repo out.ExamRepository, subjects out.SubjectDirectory) in.ListSubjectExams {
	return &listSubjectExamsService{repo: repo, subjects: subjects}
}

func (s *listSubjectExamsService) Execute(ctx context.Context, subjectID string) ([]domain.ExamSummary, error) {
	alive, err := s.subjects.IsAlive(ctx, subjectID)
	if err != nil {
		return nil, err
	}
	if !alive {
		return nil, domain.ErrNotFound
	}
	return s.repo.ListBySubject(ctx, subjectID)
}

// ── Update ──────────────────────────────────────────────────────────────────

type updateExamService struct {
	repo  out.ExamRepository
	bank  out.QuestionBank
	clock out.Clock
}

func NewUpdateExamService(repo out.ExamRepository, bank out.QuestionBank, clock out.Clock) in.UpdateExam {
	return &updateExamService{repo: repo, bank: bank, clock: clock}
}

func (s *updateExamService) Execute(ctx context.Context, id string, cmd in.UpdateExamCommand) (domain.ExamSummary, error) {
	if cmd.Version < 1 {
		return domain.ExamSummary{}, domain.SingleViolation("version", "required", "version must be the integer version the client read (>= 1)")
	}
	if cmd.Edit == (domain.ExamEdit{}) {
		return domain.ExamSummary{}, domain.SingleViolation("body", "required",
			"at least one of name, questionCount, timeLimitMin or passPercent must be provided")
	}
	exam, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.ExamSummary{}, err
	}
	if exam.Version != cmd.Version { // friendly pre-check; the UPDATE itself is the guarantee
		return domain.ExamSummary{}, domain.NewConflict("version", domain.RuleStaleVersion,
			"the exam was changed since you read it; reload and re-apply your edit")
	}
	if err := exam.Edit(cmd.Edit, s.clock.Now()); err != nil {
		return domain.ExamSummary{}, err
	}
	if err := s.repo.Update(ctx, exam, cmd.Version); err != nil {
		return domain.ExamSummary{}, err
	}
	return s.repo.GetSummary(ctx, id)
}

// ── Delete ──────────────────────────────────────────────────────────────────

type deleteExamService struct {
	repo  out.ExamRepository
	clock out.Clock
}

func NewDeleteExamService(repo out.ExamRepository, clock out.Clock) in.DeleteExam {
	return &deleteExamService{repo: repo, clock: clock}
}

// Execute deletes logically. Alive questions will block it from slice 03 on (BR-E3).
func (s *deleteExamService) Execute(ctx context.Context, id string) error {
	exam, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	exam.Delete(s.clock.Now())
	return s.repo.Delete(ctx, exam)
}
