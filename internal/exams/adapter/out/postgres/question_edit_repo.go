package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// ApplyEdit holds the exam row lock for the whole transaction, the lock imports
// and exam deletes take, so edits, imports and deletes of one exam run one after
// the other and each plan sees what the previous one committed.
func (r *QuestionBankRepository) ApplyEdit(ctx context.Context, examID string, plan func(domain.Exam, domain.Bank) (*domain.EditWrite, error)) (err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	exam, err := scanExam(tx.QueryRow(ctx, aliveExams+` AND id = $1 FOR UPDATE`, examID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = domain.ErrNotFound
		}
		return err
	}
	bank, err := loadBank(ctx, tx, examID)
	if err != nil {
		return err
	}
	write, err := plan(exam, bank)
	if err != nil {
		return err
	}
	if write == nil {
		err = tx.Rollback(ctx)
		return err
	}
	if err = writeEdit(ctx, tx, bank, *write); err != nil {
		return editWriteError(err)
	}
	return tx.Commit(ctx)
}

// editWriteError maps the unique name indexes (migrations 0009 and 0010).
func editWriteError(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation {
		return err
	}
	switch pgErr.ConstraintName {
	case "exams_subject_name_alive_ux":
		return duplicateName()
	case "exam_classifications_fold_alive_ux":
		return domain.NewConflict("classifications", domain.RuleDuplicateName, "a classification with this name already exists in the exam")
	case "exam_categories_fold_alive_ux":
		return domain.NewConflict("categories", domain.RuleDuplicateName, "a category with this name already exists in the classification")
	}
	return err
}

// writeEdit executes an EditWrite in an order that never trips a unique index:
// renamed rows first park their folded name, a demoted primary is written
// before the classification that replaces it, and a merge moves the links of
// questions that already had both ends out of the way first.
func writeEdit(ctx context.Context, tx pgx.Tx, bank domain.Bank, w domain.EditWrite) error {
	run := func(sql string, args ...any) (int64, error) {
		tag, err := tx.Exec(ctx, sql, args...)
		return tag.RowsAffected(), err
	}

	if w.Exam != nil {
		n, err := run(`UPDATE exams SET name = $3, question_count = $4, time_limit_min = $5, pass_percent = $6, version = $7, updated_at = $8
			WHERE id = $1 AND version = $2 AND deleted_at IS NULL`,
			w.Exam.ID, w.ExpectedVersion, w.Exam.Name.String(), w.Exam.QuestionCount, w.Exam.TimeLimitMin, w.Exam.PassPercent, w.Exam.Version, w.Exam.UpdatedAt)
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.NewConflict("version", domain.RuleStaleVersion, "the exam was changed since you read it; reload and re-apply your edit")
		}
	}

	oldClassFold, oldCatFold := map[string]string{}, map[string]string{}
	for _, c := range bank.Classifications {
		oldClassFold[c.ID] = c.Fold
		for _, k := range c.Categories {
			oldCatFold[k.ID] = k.Fold
		}
	}
	for _, c := range w.Classifications { // park renamed folds, so names can swap
		if oldClassFold[c.ID] != c.Fold {
			if _, err := run(`UPDATE exam_classifications SET fold_name = 'parked:' || id WHERE id = $1`, c.ID); err != nil {
				return err
			}
		}
	}
	for _, k := range w.Categories {
		if oldCatFold[k.ID] != k.Fold {
			if _, err := run(`UPDATE exam_categories SET fold_name = 'parked:' || id WHERE id = $1`, k.ID); err != nil {
				return err
			}
		}
	}

	for _, c := range w.Classifications {
		n, err := run(`UPDATE exam_classifications SET name = $2, fold_name = $3, position = $4, weight_mode = $5
			WHERE id = $1 AND exam_id = $6 AND deleted_at IS NULL`, c.ID, c.Name, c.Fold, c.Position, string(c.WeightMode), w.ExamID)
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrNotFound
		}
	}
	for _, k := range w.Categories {
		n, err := run(`UPDATE exam_categories SET key = $2, name = $3, fold_name = $4, pace_questions = $5, pace_minutes = $6
			WHERE id = $1 AND deleted_at IS NULL
			  AND classification_id IN (SELECT id FROM exam_classifications WHERE exam_id = $7 AND deleted_at IS NULL)`,
			k.ID, k.Key, k.Name, k.Fold, k.PaceQuestions, k.PaceMinutes, w.ExamID)
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrNotFound
		}
	}

	if m := w.Merge; m != nil {
		if _, err := run(`UPDATE question_categories SET deleted_at = $3
			WHERE category_id = $1 AND deleted_at IS NULL
			  AND question_id IN (SELECT question_id FROM question_categories WHERE category_id = $2 AND deleted_at IS NULL)`, m.From, m.Into, w.Now); err != nil {
			return err
		}
		if _, err := run(`UPDATE question_categories SET category_id = $2 WHERE category_id = $1 AND deleted_at IS NULL`, m.From, m.Into); err != nil {
			return err
		}
		n, err := run(`UPDATE exam_categories SET deleted_at = $2 WHERE id = $1 AND deleted_at IS NULL`, m.From, w.Now)
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrNotFound
		}
	}

	for _, cw := range w.Weights {
		if _, err := run(`UPDATE exam_categories SET weight_percent = $2 WHERE id = $1 AND deleted_at IS NULL`, cw.CategoryID, cw.Percent); err != nil {
			return err
		}
	}

	if w.DeleteQuestionID != "" {
		n, err := run(`UPDATE questions SET deleted_at = $3 WHERE id = $1 AND exam_id = $2 AND deleted_at IS NULL`, w.DeleteQuestionID, w.ExamID, w.Now)
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrNotFound
		}
	}
	return nil
}
