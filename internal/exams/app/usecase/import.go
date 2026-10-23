package usecase

import (
	"context"
	"errors"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/in"
	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

type importQuestionsService struct {
	bank  out.QuestionBank
	clock out.Clock
	ids   out.IDGenerator
}

func NewImportQuestionsService(bank out.QuestionBank, clock out.Clock, ids out.IDGenerator) in.ImportQuestions {
	return &importQuestionsService{bank: bank, clock: clock, ids: ids}
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

	plan := func(bank domain.Bank) domain.ImportPlan {
		return domain.PlanImport(domain.PlanInput{
			ExamID: cmd.ExamID, Bank: bank, File: *cmd.File, Primary: cmd.Primary,
			NewID: s.ids.NewID, Now: s.clock.Now(),
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
	err := s.bank.ApplyImport(ctx, cmd.ExamID, func(bank domain.Bank) (*domain.ImportWrite, error) {
		planned = plan(bank)
		return planned.Write, nil // nil when the plan has errors: nothing is written
	})
	if err != nil {
		return domain.ImportReport{}, err
	}
	report := planned.Report
	report.Applied = planned.Write != nil
	return report, nil
}
