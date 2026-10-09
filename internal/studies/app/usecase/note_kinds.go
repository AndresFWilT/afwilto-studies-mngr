package usecase

import (
	"context"

	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/app/port/out"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// ── Create ──────────────────────────────────────────────────────────────────

type createNoteKindService struct {
	repo  out.NoteKindRepository
	clock out.Clock
	ids   out.IDGenerator
}

func NewCreateNoteKindService(repo out.NoteKindRepository, clock out.Clock, ids out.IDGenerator) in.CreateNoteKind {
	return &createNoteKindService{repo: repo, clock: clock, ids: ids}
}

func (s *createNoteKindService) Execute(ctx context.Context, cmd in.CreateNoteKindCommand) (domain.NoteKindView, error) {
	name, err := domain.NewNoteKindName(cmd.Name)
	if err != nil {
		return domain.NoteKindView{}, err
	}
	kind := domain.NewNoteKind(s.ids.NewID(), name, s.clock.Now())
	if err := s.repo.Create(ctx, kind); err != nil {
		return domain.NoteKindView{}, err
	}
	return s.repo.GetView(ctx, kind.ID)
}

// ── Get / List ──────────────────────────────────────────────────────────────

type getNoteKindService struct{ repo out.NoteKindRepository }

func NewGetNoteKindService(repo out.NoteKindRepository) in.GetNoteKind {
	return &getNoteKindService{repo: repo}
}

func (s *getNoteKindService) Execute(ctx context.Context, id string) (domain.NoteKindView, error) {
	return s.repo.GetView(ctx, id)
}

type listNoteKindsService struct{ repo out.NoteKindRepository }

func NewListNoteKindsService(repo out.NoteKindRepository) in.ListNoteKinds {
	return &listNoteKindsService{repo: repo}
}

func (s *listNoteKindsService) Execute(ctx context.Context) ([]domain.NoteKindView, error) {
	return s.repo.List(ctx)
}

// ── Rename ──────────────────────────────────────────────────────────────────

type renameNoteKindService struct {
	repo  out.NoteKindRepository
	clock out.Clock
}

func NewRenameNoteKindService(repo out.NoteKindRepository, clock out.Clock) in.RenameNoteKind {
	return &renameNoteKindService{repo: repo, clock: clock}
}

func (s *renameNoteKindService) Execute(ctx context.Context, id string, cmd in.RenameNoteKindCommand) (domain.NoteKindView, error) {
	kind, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.NoteKindView{}, err
	}
	if kind.Name, err = domain.NewNoteKindName(cmd.Name); err != nil {
		return domain.NoteKindView{}, err
	}
	kind.UpdatedAt = s.clock.Now()
	if err := s.repo.Update(ctx, kind); err != nil {
		return domain.NoteKindView{}, err
	}
	return s.repo.GetView(ctx, id)
}

// ── Delete ──────────────────────────────────────────────────────────────────

type deleteNoteKindService struct {
	repo  out.NoteKindRepository
	clock out.Clock
}

func NewDeleteNoteKindService(repo out.NoteKindRepository, clock out.Clock) in.DeleteNoteKind {
	return &deleteNoteKindService{repo: repo, clock: clock}
}

// Execute deletes logically. The "used by alive notes" block of BR-DEL1 joins
// in Slice 05, when the notes table exists.
func (s *deleteNoteKindService) Execute(ctx context.Context, id string) error {
	kind, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	kind.Delete(s.clock.Now())
	return s.repo.Update(ctx, kind)
}
