package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// ListQuestions is the bank browser (slice 04 §1.1). The filters are SQL
// predicates combined with AND; the order is origin set, origin number, then
// key, with questions lacking a set or number last. Sets compare ignoring case
// (then exactly), numbers as numbers, keys in byte order.
func (r *QuestionBankRepository) ListQuestions(ctx context.Context, examID string, f domain.QuestionFilter) ([]domain.QuestionSummary, error) {
	var alive bool
	if err := r.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exams WHERE id = $1 AND deleted_at IS NULL)`, examID).Scan(&alive); err != nil {
		return nil, err
	}
	if !alive {
		return nil, domain.ErrNotFound
	}

	where := []string{"q.exam_id = $1", "q.deleted_at IS NULL"}
	args := []any{examID}
	arg := func(v any) string {
		args = append(args, v)
		return fmt.Sprintf("$%d", len(args))
	}
	if f.CategoryID != "" {
		where = append(where, `EXISTS (SELECT 1 FROM question_categories qc JOIN exam_categories c ON c.id = qc.category_id
			WHERE qc.question_id = q.id AND qc.category_id = `+arg(f.CategoryID)+` AND qc.deleted_at IS NULL AND c.deleted_at IS NULL)`)
	}
	if f.Origin != "" {
		where = append(where, `q.origin->>'set' = `+arg(f.Origin))
	}
	if f.Difficulty != "" {
		where = append(where, `q.difficulty = `+arg(f.Difficulty))
	}
	if f.Text != "" { // position(), not LIKE: % and _ in the text are plain characters
		where = append(where, `position(lower(`+arg(f.Text)+`) in lower(q.stem)) > 0`)
	}
	if f.Unclassified {
		where = append(where, `NOT EXISTS (SELECT 1 FROM question_categories qc
			JOIN exam_categories c ON c.id = qc.category_id JOIN exam_classifications k ON k.id = c.classification_id
			WHERE qc.question_id = q.id AND qc.deleted_at IS NULL AND c.deleted_at IS NULL AND k.deleted_at IS NULL AND k.position = 0)`)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT q.id, q.key, q.type, q.stem, q.difficulty, q.origin, q.version, q.updated_at
		FROM questions q WHERE `+strings.Join(where, " AND ")+`
		ORDER BY lower(q.origin->>'set') COLLATE "C" NULLS LAST, (q.origin->>'set') COLLATE "C" NULLS LAST,
		         ((q.origin->>'number')::int) NULLS LAST, q.key COLLATE "C"`, args...)
	if err != nil {
		return nil, err
	}
	summaries := []domain.QuestionSummary{}
	var ids []string
	for rows.Next() {
		var (
			q          domain.Question
			typ        string
			difficulty *string
			origin     []byte
			updated    time.Time
		)
		if err := rows.Scan(&q.ID, &q.Key, &typ, &q.Stem, &difficulty, &origin, &q.Version, &updated); err != nil {
			rows.Close()
			return nil, err
		}
		q.Type, q.Difficulty, q.UpdatedAt = domain.QuestionType(typ), deref(difficulty), updated
		if q.Origin, err = decodeOrigin(origin); err != nil {
			rows.Close()
			return nil, fmt.Errorf("question %s origin: %w", q.Key, err)
		}
		summaries = append(summaries, domain.SummarizeQuestion(q))
		ids = append(ids, q.ID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	refs, err := loadCategoryRefs(ctx, r.pool, ids)
	if err != nil {
		return nil, err
	}
	for i := range summaries {
		summaries[i].Categories = refs[summaries[i].ID]
	}
	return summaries, nil
}

// loadCategoryRefs reads the alive category links of the given questions.
func loadCategoryRefs(ctx context.Context, q queryer, questionIDs []string) (map[string][]domain.CategoryRef, error) {
	out := map[string][]domain.CategoryRef{}
	if len(questionIDs) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT qc.question_id, k.name, c.name
		FROM question_categories qc
		JOIN exam_categories c ON c.id = qc.category_id
		JOIN exam_classifications k ON k.id = c.classification_id
		WHERE qc.question_id = ANY($1::uuid[]) AND qc.deleted_at IS NULL AND c.deleted_at IS NULL AND k.deleted_at IS NULL
		ORDER BY qc.id`, questionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var ref domain.CategoryRef
		if err := rows.Scan(&id, &ref.Classification, &ref.Category); err != nil {
			return nil, err
		}
		out[id] = append(out[id], ref)
	}
	return out, rows.Err()
}

// GetQuestion reads one alive question of an alive exam with its options in
// stored order.
func (r *QuestionBankRepository) GetQuestion(ctx context.Context, id string) (domain.Question, error) {
	var (
		q           domain.Question
		typ         string
		explanation *string
		difficulty  *string
		origin      []byte
	)
	err := r.pool.QueryRow(ctx, `
		SELECT q.exam_id, q.key, q.type, q.stem, q.explanation, q.difficulty, q.source_kind, q.source_name, q.origin,
		       q.stem_fingerprint, q.version, q.created_at, q.updated_at
		FROM questions q JOIN exams e ON e.id = q.exam_id AND e.deleted_at IS NULL
		WHERE q.id = $1 AND q.deleted_at IS NULL`, id).
		Scan(&q.ExamID, &q.Key, &typ, &q.Stem, &explanation, &difficulty, &q.Source.Kind, &q.Source.Name, &origin,
			&q.Fingerprint, &q.Version, &q.CreatedAt, &q.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Question{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Question{}, err
	}
	q.ID, q.Type, q.Explanation, q.Difficulty = id, domain.QuestionType(typ), deref(explanation), deref(difficulty)
	if q.Origin, err = decodeOrigin(origin); err != nil {
		return domain.Question{}, fmt.Errorf("question %s origin: %w", q.Key, err)
	}

	rows, err := r.pool.Query(ctx, `
		SELECT text, image, alt, correct, rationale FROM question_options
		WHERE question_id = $1 AND deleted_at IS NULL ORDER BY position`, id)
	if err != nil {
		return domain.Question{}, err
	}
	for rows.Next() {
		var text, image, alt, rationale *string
		var correct bool
		if err := rows.Scan(&text, &image, &alt, &correct, &rationale); err != nil {
			rows.Close()
			return domain.Question{}, err
		}
		q.Options = append(q.Options, domain.Option{Text: deref(text), Image: deref(image), Alt: deref(alt), Correct: correct, Rationale: deref(rationale)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.Question{}, err
	}

	refs, err := loadCategoryRefs(ctx, r.pool, []string{id})
	if err != nil {
		return domain.Question{}, err
	}
	q.Categories = refs[id]
	return q, nil
}
