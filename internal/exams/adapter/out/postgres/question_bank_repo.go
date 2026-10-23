package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// QuestionBankRepository persists classifications, categories and questions
// (slice 03 red stub).
type QuestionBankRepository struct{ pool *pgxpool.Pool }

func NewQuestionBankRepository(pool *pgxpool.Pool) *QuestionBankRepository {
	return &QuestionBankRepository{pool: pool}
}

func (r *QuestionBankRepository) LoadBank(ctx context.Context, examID string) (domain.Bank, error) {
	return domain.Bank{}, errors.New("not implemented")
}

func (r *QuestionBankRepository) ApplyImport(ctx context.Context, examID string, plan func(domain.Bank) (*domain.ImportWrite, error)) error {
	return errors.New("not implemented")
}
