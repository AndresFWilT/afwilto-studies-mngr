package usecase

import (
	"context"
	"errors"

	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/app/port/out"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// requireAliveCategory is the friendly pre-check of BR-DEL2; the database
// trigger remains the guarantee. A missing or deleted category is a 422 on
// categoryId, not a 404 on the subject.
func requireAliveCategory(ctx context.Context, categories out.CategoryRepository, id string) error {
	if _, err := categories.Get(ctx, id); errors.Is(err, domain.ErrNotFound) {
		return domain.SingleViolation("categoryId", "not_found", "category does not exist")
	} else if err != nil {
		return err
	}
	return nil
}

// ── Create ──────────────────────────────────────────────────────────────────

type createSubjectService struct {
	repo       out.SubjectRepository
	categories out.CategoryRepository
	clock      out.Clock
	ids        out.IDGenerator
}

func NewCreateSubjectService(repo out.SubjectRepository, categories out.CategoryRepository, clock out.Clock, ids out.IDGenerator) in.CreateSubject {
	return &createSubjectService{repo: repo, categories: categories, clock: clock, ids: ids}
}

func (s *createSubjectService) Execute(ctx context.Context, cmd in.CreateSubjectCommand) (domain.SubjectView, error) {
	name, err := domain.NewSubjectName(cmd.Name)
	if err != nil {
		return domain.SubjectView{}, err
	}
	description, err := domain.NewSubjectDescription(cmd.Description)
	if err != nil {
		return domain.SubjectView{}, err
	}
	if err := requireAliveCategory(ctx, s.categories, cmd.CategoryID); err != nil {
		return domain.SubjectView{}, err
	}
	subject := domain.NewSubject(s.ids.NewID(), cmd.CategoryID, name, description, s.clock.Now())
	if err := s.repo.Create(ctx, subject); err != nil {
		return domain.SubjectView{}, err
	}
	return s.repo.GetView(ctx, subject.ID)
}

// ── Get / List ──────────────────────────────────────────────────────────────

type getSubjectService struct{ repo out.SubjectRepository }

func NewGetSubjectService(repo out.SubjectRepository) in.GetSubject {
	return &getSubjectService{repo: repo}
}

func (s *getSubjectService) Execute(ctx context.Context, id string) (domain.SubjectView, error) {
	return s.repo.GetView(ctx, id)
}

type listSubjectsService struct{ repo out.SubjectRepository }

func NewListSubjectsService(repo out.SubjectRepository) in.ListSubjects {
	return &listSubjectsService{repo: repo}
}

func (s *listSubjectsService) Execute(ctx context.Context, categoryID *string) ([]domain.SubjectView, error) {
	return s.repo.List(ctx, categoryID)
}

// ── Update ──────────────────────────────────────────────────────────────────

type updateSubjectService struct {
	repo       out.SubjectRepository
	categories out.CategoryRepository
	clock      out.Clock
}

func NewUpdateSubjectService(repo out.SubjectRepository, categories out.CategoryRepository, clock out.Clock) in.UpdateSubject {
	return &updateSubjectService{repo: repo, categories: categories, clock: clock}
}

func (s *updateSubjectService) Execute(ctx context.Context, id string, cmd in.UpdateSubjectCommand) (domain.SubjectView, error) {
	subject, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.SubjectView{}, err
	}

	if cmd.Name.Set {
		if subject.Name, err = domain.NewSubjectName(deref(cmd.Name.Value)); err != nil {
			return domain.SubjectView{}, err
		}
	}
	if cmd.Description.Set {
		if subject.Description, err = domain.NewSubjectDescription(cmd.Description.Value); err != nil {
			return domain.SubjectView{}, err
		}
	}
	if cmd.CategoryID.Set {
		if cmd.CategoryID.Value == nil {
			return domain.SubjectView{}, domain.SingleViolation("categoryId", "required", "categoryId must not be null")
		}
		if err := requireAliveCategory(ctx, s.categories, *cmd.CategoryID.Value); err != nil {
			return domain.SubjectView{}, err
		}
		subject.CategoryID = *cmd.CategoryID.Value
	}

	subject.UpdatedAt = s.clock.Now()
	if err := s.repo.Update(ctx, subject); err != nil {
		return domain.SubjectView{}, err
	}
	return s.repo.GetView(ctx, id)
}

// ── Delete ──────────────────────────────────────────────────────────────────

type deleteSubjectService struct {
	repo  out.SubjectRepository
	clock out.Clock
}

func NewDeleteSubjectService(repo out.SubjectRepository, clock out.Clock) in.DeleteSubject {
	return &deleteSubjectService{repo: repo, clock: clock}
}

// Execute deletes logically. The "alive notes" block of BR-DEL1 joins in
// Slice 05, when the notes table exists.
func (s *deleteSubjectService) Execute(ctx context.Context, id string) error {
	subject, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	subject.Delete(s.clock.Now())
	return s.repo.Update(ctx, subject)
}
