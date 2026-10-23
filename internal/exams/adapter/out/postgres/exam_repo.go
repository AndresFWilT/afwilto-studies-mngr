package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

var errNotImplemented = errors.New("not implemented")

type ExamRepository struct {
	pool *pgxpool.Pool
}

func NewExamRepository(pool *pgxpool.Pool) *ExamRepository {
	return &ExamRepository{pool: pool}
}

func (r *ExamRepository) Create(ctx context.Context, e domain.Exam) error { return errNotImplemented }

func (r *ExamRepository) Update(ctx context.Context, e domain.Exam, expectedVersion int) error {
	return errNotImplemented
}

func (r *ExamRepository) Delete(ctx context.Context, e domain.Exam) error { return errNotImplemented }

func (r *ExamRepository) Get(ctx context.Context, id string) (domain.Exam, error) {
	return domain.Exam{}, errNotImplemented
}

func (r *ExamRepository) GetSummary(ctx context.Context, id string) (domain.ExamSummary, error) {
	return domain.ExamSummary{}, errNotImplemented
}

func (r *ExamRepository) ListBySubject(ctx context.Context, subjectID string) ([]domain.ExamSummary, error) {
	return nil, errNotImplemented
}
