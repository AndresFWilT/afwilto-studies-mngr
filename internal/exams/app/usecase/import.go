package usecase

import (
	"bytes"
	"context"
	"errors"
	"fmt"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

type importQuestionsService struct {
	bank    out.QuestionBank
	clock   out.Clock
	ids     out.IDGenerator
	index   out.MediaIndex
	storage out.MediaStorage
}

// ImportOption wires an optional collaborator of the import.
type ImportOption func(*importQuestionsService)

// WithMedia lets the import resolve stored hashes (media:<sha256>) and store
// the images of a zip.
func WithMedia(index out.MediaIndex, storage out.MediaStorage) ImportOption {
	return func(s *importQuestionsService) { s.index, s.storage = index, storage }
}

func NewImportQuestionsService(bank out.QuestionBank, clock out.Clock, ids out.IDGenerator, opts ...ImportOption) in.ImportQuestions {
	s := &importQuestionsService{bank: bank, clock: clock, ids: ids}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Execute runs stages 3 to 5 of the import pipeline (slice 03 §1.3). A dry run
// plans against a read of the bank; an apply plans inside the bank's
// transaction, so the plan is made against the very state it writes to.
func (s *importQuestionsService) Execute(ctx context.Context, cmd in.ImportQuestionsCommand) (domain.ImportReport, error) {
	if cmd.File == nil && len(cmd.FileErrors) == 0 {
		return domain.ImportReport{}, errors.New("import command has neither a parsed file nor file errors")
	}

	if len(cmd.FileErrors) > 0 { // stages 1-2 failed upstream; the exam must still exist
		if _, err := s.bank.LoadBank(ctx, cmd.ExamID); err != nil {
			return domain.ImportReport{}, err
		}
		report := domain.PlanFailure(cmd.FileErrors).Report
		report.DryRun = cmd.DryRun
		return report, nil
	}

	known, err := s.storedHashes(ctx, cmd.File)
	if err != nil {
		return domain.ImportReport{}, err
	}
	plan := func(bank domain.Bank) domain.ImportPlan {
		return domain.PlanImport(domain.PlanInput{
			ExamID: cmd.ExamID, Bank: bank, File: *cmd.File, Primary: cmd.Primary,
			NewID: s.ids.NewID, Now: s.clock.Now(), Archive: cmd.Archive, KnownMedia: known,
		})
	}

	if cmd.DryRun {
		bank, err := s.bank.LoadBank(ctx, cmd.ExamID)
		if err != nil {
			return domain.ImportReport{}, err
		}
		report := plan(bank).Report
		report.DryRun = true
		return report, nil
	}

	var planned domain.ImportPlan
	err = s.bank.ApplyImport(ctx, cmd.ExamID, func(bank domain.Bank) (*domain.ImportWrite, error) {
		planned = plan(bank)
		if planned.Write == nil { // the plan has errors: nothing is written, files included
			return nil, nil
		}
		// The files go to disk before the transaction commits. If it then
		// rolls back, the orphan files are harmless: they are content-addressed
		// and the next import of the same image reuses them.
		if err := s.storeFiles(ctx, planned.Write.Media); err != nil {
			return nil, err
		}
		return planned.Write, nil
	})
	if errors.Is(err, domain.ErrInvalidText) {
		// The planner rejects NUL bytes, so this is text it could not foresee;
		// it is still bad input (422), not a server fault.
		return domain.PlanFailure([]domain.ImportError{{Path: "$", Rule: domain.RuleInvalidText,
			Message: "the database cannot store part of the file's text (a NUL character, U+0000)"}}).Report, nil
	}
	if err != nil {
		return domain.ImportReport{}, err
	}
	report := planned.Report
	report.Applied = planned.Write != nil
	return report, nil
}

// storedHashes finds which of the file's media:<sha256> references are stored.
// A failed lookup is an error, never an empty answer.
func (s *importQuestionsService) storedHashes(ctx context.Context, file *domain.QuestionFile) (map[string]bool, error) {
	var shas []string
	seen := map[string]bool{}
	for _, r := range domain.MediaRefs(file.Questions) {
		if domain.IsMediaSHA(r.Ref) && !seen[r.Ref] {
			seen[r.Ref] = true
			shas = append(shas, r.Ref)
		}
	}
	known := map[string]bool{}
	if len(shas) == 0 || s.index == nil {
		return known, nil
	}
	found, err := s.index.Find(ctx, shas)
	if err != nil {
		return nil, fmt.Errorf("looking up stored media: %w", err)
	}
	for sha := range found {
		known[sha] = true
	}
	return known, nil
}

func (s *importQuestionsService) storeFiles(ctx context.Context, files []domain.MediaFile) error {
	if len(files) > 0 && s.storage == nil {
		return errors.New("the import has images to store but no media storage is wired")
	}
	for _, f := range files {
		if err := s.storage.Put(ctx, f.Media.SHA256, bytes.NewReader(f.Data)); err != nil {
			return fmt.Errorf("storing media %s: %w", f.Media.SHA256, err)
		}
	}
	return nil
}
