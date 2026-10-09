package usecase

import (
	"context"
	"errors"

	"github.com/afwilto/studies-mngr/internal/studies/app/port/in"
	"github.com/afwilto/studies-mngr/internal/studies/app/port/out"
	"github.com/afwilto/studies-mngr/internal/studies/domain"
)

// requireAliveKind is the friendly pre-check of BR-DEL2 for kindId; the
// database trigger remains the guarantee.
func requireAliveKind(ctx context.Context, kinds out.NoteKindRepository, id string) error {
	if _, err := kinds.Get(ctx, id); errors.Is(err, domain.ErrNotFound) {
		return domain.SingleViolation("kindId", "not_found", "note kind does not exist")
	} else if err != nil {
		return err
	}
	return nil
}

func requireAliveSubject(ctx context.Context, subjects out.SubjectRepository, id string) error {
	if _, err := subjects.Get(ctx, id); errors.Is(err, domain.ErrNotFound) {
		return domain.SingleViolation("subjectId", "not_found", "subject does not exist")
	} else if err != nil {
		return err
	}
	return nil
}

// ── Create ──────────────────────────────────────────────────────────────────

type createNoteService struct {
	repo     out.NoteRepository
	subjects out.SubjectRepository
	kinds    out.NoteKindRepository
	clock    out.Clock
	ids      out.IDGenerator
}

func NewCreateNoteService(repo out.NoteRepository, subjects out.SubjectRepository, kinds out.NoteKindRepository, clock out.Clock, ids out.IDGenerator) in.CreateNote {
	return &createNoteService{repo: repo, subjects: subjects, kinds: kinds, clock: clock, ids: ids}
}

func (s *createNoteService) Execute(ctx context.Context, cmd in.CreateNoteCommand) (domain.NoteView, error) {
	title, err := domain.NewNoteTitle(cmd.Title)
	if err != nil {
		return domain.NoteView{}, err
	}
	content, err := domain.NewMarkdownContent(deref(cmd.Content))
	if err != nil {
		return domain.NoteView{}, err
	}
	tags, err := domain.NewTagSet(cmd.Tags)
	if err != nil {
		return domain.NoteView{}, err
	}
	links, err := domain.NewSlabLinks(cmd.SlabLinks)
	if err != nil {
		return domain.NoteView{}, err
	}
	// The subject is the resource the request addresses: unknown means 404.
	if _, err := s.subjects.Get(ctx, cmd.SubjectID); err != nil {
		return domain.NoteView{}, err
	}
	if err := requireAliveKind(ctx, s.kinds, cmd.KindID); err != nil {
		return domain.NoteView{}, err
	}
	note := domain.NewNote(s.ids.NewID(), cmd.SubjectID, cmd.KindID, title, content, s.clock.Now())
	note.Tags, note.SlabLinks = tags, links
	if err := s.repo.Create(ctx, note); err != nil {
		return domain.NoteView{}, err
	}
	return s.repo.GetView(ctx, note.ID)
}

// ── Get / List ──────────────────────────────────────────────────────────────

type getNoteService struct{ repo out.NoteRepository }

func NewGetNoteService(repo out.NoteRepository) in.GetNote { return &getNoteService{repo: repo} }

func (s *getNoteService) Execute(ctx context.Context, id string) (domain.NoteView, error) {
	return s.repo.GetView(ctx, id)
}

type listSubjectNotesService struct {
	repo     out.NoteRepository
	subjects out.SubjectRepository
}

func NewListSubjectNotesService(repo out.NoteRepository, subjects out.SubjectRepository) in.ListSubjectNotes {
	return &listSubjectNotesService{repo: repo, subjects: subjects}
}

func (s *listSubjectNotesService) Execute(ctx context.Context, subjectID string, tag *string, sort domain.SortSpec) ([]domain.NoteView, error) {
	var normalized *string
	if tag != nil {
		t, err := domain.NewTag(*tag)
		if err != nil {
			return nil, err
		}
		normalized = &t
	}
	if _, err := s.subjects.Get(ctx, subjectID); err != nil {
		return nil, err
	}
	return s.repo.ListBySubject(ctx, subjectID, normalized, sort)
}

type listNotesByTagService struct{ repo out.NoteRepository }

func NewListNotesByTagService(repo out.NoteRepository) in.ListNotesByTag {
	return &listNotesByTagService{repo: repo}
}

func (s *listNotesByTagService) Execute(ctx context.Context, tag string, sort domain.SortSpec) ([]domain.NoteView, error) {
	normalized, err := domain.NewTag(tag)
	if err != nil {
		return nil, err
	}
	return s.repo.ListByTag(ctx, normalized, sort)
}

type listTagsService struct{ query out.TagQuery }

func NewListTagsService(query out.TagQuery) in.ListTags { return &listTagsService{query: query} }

func (s *listTagsService) Execute(ctx context.Context) ([]domain.TagCount, error) {
	return s.query.List(ctx)
}

// ── Update ──────────────────────────────────────────────────────────────────

type updateNoteService struct {
	repo     out.NoteRepository
	subjects out.SubjectRepository
	kinds    out.NoteKindRepository
	clock    out.Clock
}

func NewUpdateNoteService(repo out.NoteRepository, subjects out.SubjectRepository, kinds out.NoteKindRepository, clock out.Clock) in.UpdateNote {
	return &updateNoteService{repo: repo, subjects: subjects, kinds: kinds, clock: clock}
}

func requiredNull(field string) error {
	return domain.SingleViolation(field, "required", field+" must not be null")
}

func (s *updateNoteService) Execute(ctx context.Context, id string, cmd in.UpdateNoteCommand) (domain.NoteView, error) {
	if cmd.Version < 1 {
		return domain.NoteView{}, domain.SingleViolation("version", "required", "version must be the integer version the client read (>= 1)")
	}
	if !cmd.Title.Set && !cmd.KindID.Set && !cmd.SubjectID.Set && !cmd.Content.Set && !cmd.Tags.Set && !cmd.SlabLinks.Set {
		return domain.NoteView{}, domain.SingleViolation("body", "required", "at least one of title, kindId, subjectId, content, tags or slabLinks must be provided")
	}
	note, err := s.repo.Get(ctx, id)
	if err != nil {
		return domain.NoteView{}, err
	}
	if note.Version != cmd.Version { // friendly pre-check; the UPDATE itself is the guarantee
		return domain.NoteView{}, staleVersion()
	}

	if cmd.Title.Set {
		if cmd.Title.Value == nil {
			return domain.NoteView{}, requiredNull("title")
		}
		if note.Title, err = domain.NewNoteTitle(*cmd.Title.Value); err != nil {
			return domain.NoteView{}, err
		}
	}
	if cmd.Content.Set {
		if cmd.Content.Value == nil {
			return domain.NoteView{}, requiredNull("content")
		}
		if note.Content, err = domain.NewMarkdownContent(*cmd.Content.Value); err != nil {
			return domain.NoteView{}, err
		}
	}
	if cmd.Tags.Set {
		if cmd.Tags.Value == nil {
			return domain.NoteView{}, requiredNull("tags")
		}
		if note.Tags, err = domain.NewTagSet(*cmd.Tags.Value); err != nil {
			return domain.NoteView{}, err
		}
	}
	if cmd.SlabLinks.Set {
		if cmd.SlabLinks.Value == nil {
			return domain.NoteView{}, requiredNull("slabLinks")
		}
		if note.SlabLinks, err = domain.NewSlabLinks(*cmd.SlabLinks.Value); err != nil {
			return domain.NoteView{}, err
		}
	}
	if cmd.KindID.Set {
		if cmd.KindID.Value == nil {
			return domain.NoteView{}, requiredNull("kindId")
		}
		if err := requireAliveKind(ctx, s.kinds, *cmd.KindID.Value); err != nil {
			return domain.NoteView{}, err
		}
		note.KindID = *cmd.KindID.Value
	}
	if cmd.SubjectID.Set {
		if cmd.SubjectID.Value == nil {
			return domain.NoteView{}, requiredNull("subjectId")
		}
		if err := requireAliveSubject(ctx, s.subjects, *cmd.SubjectID.Value); err != nil {
			return domain.NoteView{}, err
		}
		note.SubjectID = *cmd.SubjectID.Value
	}

	note.Touch(s.clock.Now())
	if err := s.repo.Update(ctx, note, cmd.Version); err != nil {
		return domain.NoteView{}, err
	}
	return s.repo.GetView(ctx, id)
}

func staleVersion() error {
	return domain.NewConflict("version", domain.RuleStaleVersion,
		"the note was changed since you read it; reload and re-apply your edit")
}

// ── Delete ──────────────────────────────────────────────────────────────────

type deleteNoteService struct {
	repo  out.NoteRepository
	clock out.Clock
}

func NewDeleteNoteService(repo out.NoteRepository, clock out.Clock) in.DeleteNote {
	return &deleteNoteService{repo: repo, clock: clock}
}

func (s *deleteNoteService) Execute(ctx context.Context, id string) error {
	note, err := s.repo.Get(ctx, id)
	if err != nil {
		return err
	}
	note.Delete(s.clock.Now())
	return s.repo.Delete(ctx, note)
}
