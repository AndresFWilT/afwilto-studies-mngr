package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// QuestionBankRepository persists classifications, categories, questions,
// options and category links (Spec 18 §2, migration 0010).
type QuestionBankRepository struct{ pool *pgxpool.Pool }

func NewQuestionBankRepository(pool *pgxpool.Pool) *QuestionBankRepository {
	return &QuestionBankRepository{pool: pool}
}

// queryer is what loadBank needs from a pool or a transaction.
type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// readSnapshot runs fn in one REPEATABLE READ, READ ONLY transaction, so every
// statement of a multi-query read sees the same committed state (M1).
func readSnapshot(ctx context.Context, pool *pgxpool.Pool, fn func(q queryer) error) error {
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (r *QuestionBankRepository) LoadBank(ctx context.Context, examID string) (bank domain.Bank, err error) {
	err = readSnapshot(ctx, r.pool, func(q queryer) error {
		var alive bool
		if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exams WHERE id = $1 AND deleted_at IS NULL)`, examID).Scan(&alive); err != nil {
			return err
		}
		if !alive {
			return domain.ErrNotFound
		}
		var loadErr error
		bank, loadErr = loadBank(ctx, q, examID)
		return loadErr
	})
	if err != nil {
		return domain.Bank{}, err
	}
	return bank, nil
}

// loadClassifications reads the alive classifications of an exam with their
// alive categories and the alive question count of each category.
func loadClassifications(ctx context.Context, q queryer, examID string) ([]domain.Classification, error) {
	rows, err := q.Query(ctx, `
		SELECT id, name, fold_name, position, weight_mode
		FROM exam_classifications WHERE exam_id = $1 AND deleted_at IS NULL ORDER BY ord`, examID)
	if err != nil {
		return nil, err
	}
	var classes []domain.Classification
	index := map[string]int{}
	for rows.Next() {
		c := domain.Classification{ExamID: examID}
		var mode string
		if err := rows.Scan(&c.ID, &c.Name, &c.Fold, &c.Position, &mode); err != nil {
			rows.Close()
			return nil, err
		}
		c.WeightMode = domain.WeightMode(mode)
		index[c.ID] = len(classes)
		classes = append(classes, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = q.Query(ctx, `
		SELECT c.id, c.classification_id, c.key, c.name, c.fold_name, c.weight_percent, c.pace_questions, c.pace_minutes,
		       (SELECT count(*) FROM question_categories qc JOIN questions qu ON qu.id = qc.question_id
		        WHERE qc.category_id = c.id AND qc.deleted_at IS NULL AND qu.deleted_at IS NULL)
		FROM exam_categories c
		JOIN exam_classifications k ON k.id = c.classification_id
		WHERE k.exam_id = $1 AND k.deleted_at IS NULL AND c.deleted_at IS NULL
		ORDER BY c.ord`, examID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var c domain.Category
		if err := rows.Scan(&c.ID, &c.ClassificationID, &c.Key, &c.Name, &c.Fold, &c.WeightPercent, &c.PaceQuestions, &c.PaceMinutes, &c.QuestionCount); err != nil {
			return nil, err
		}
		i, ok := index[c.ClassificationID]
		if !ok { // not loaded by the first query: skip, never attach to element 0
			continue
		}
		classes[i].Categories = append(classes[i].Categories, c)
	}
	return classes, rows.Err()
}

type originJSON struct {
	Set     string `json:"set,omitempty"`
	Number  int    `json:"number,omitempty"`
	NoteRef string `json:"noteRef,omitempty"`
}

// decodeOrigin reads the origin column; nil for NULL.
func decodeOrigin(raw []byte) (*domain.Origin, error) {
	if raw == nil {
		return nil, nil
	}
	var o originJSON
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, err
	}
	return &domain.Origin{Set: o.Set, Number: o.Number, NoteRef: o.NoteRef}, nil
}

func loadBank(ctx context.Context, q queryer, examID string) (domain.Bank, error) {
	classes, err := loadClassifications(ctx, q, examID)
	if err != nil {
		return domain.Bank{}, err
	}
	bank := domain.Bank{Classifications: classes}

	rows, err := q.Query(ctx, `
		SELECT id, key, type, stem, explanation, difficulty, source_kind, source_name, origin, stem_fingerprint, version, created_at, updated_at
		FROM questions WHERE exam_id = $1 AND deleted_at IS NULL ORDER BY ord`, examID)
	if err != nil {
		return domain.Bank{}, err
	}
	index := map[string]int{}
	for rows.Next() {
		var (
			qu          domain.Question
			typ         string
			explanation *string
			difficulty  *string
			origin      []byte
		)
		if err := rows.Scan(&qu.ID, &qu.Key, &typ, &qu.Stem, &explanation, &difficulty, &qu.Source.Kind, &qu.Source.Name,
			&origin, &qu.Fingerprint, &qu.Version, &qu.CreatedAt, &qu.UpdatedAt); err != nil {
			rows.Close()
			return domain.Bank{}, err
		}
		qu.ExamID, qu.Type = examID, domain.QuestionType(typ)
		if explanation != nil {
			qu.Explanation = *explanation
		}
		if difficulty != nil {
			qu.Difficulty = *difficulty
		}
		if qu.Origin, err = decodeOrigin(origin); err != nil {
			rows.Close()
			return domain.Bank{}, fmt.Errorf("question %s origin: %w", qu.Key, err)
		}
		index[qu.ID] = len(bank.Questions)
		bank.Questions = append(bank.Questions, qu)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.Bank{}, err
	}

	rows, err = q.Query(ctx, `
		SELECT o.question_id, o.text, o.image, o.alt, o.correct, o.rationale
		FROM question_options o JOIN questions qu ON qu.id = o.question_id
		WHERE qu.exam_id = $1 AND qu.deleted_at IS NULL AND o.deleted_at IS NULL
		ORDER BY o.question_id, o.position`, examID)
	if err != nil {
		return domain.Bank{}, err
	}
	for rows.Next() {
		var id string
		var text, image, alt, rationale *string
		var correct bool
		if err := rows.Scan(&id, &text, &image, &alt, &correct, &rationale); err != nil {
			rows.Close()
			return domain.Bank{}, err
		}
		opt := domain.Option{Correct: correct, Text: deref(text), Image: deref(image), Alt: deref(alt), Rationale: deref(rationale)}
		i, ok := index[id]
		if !ok { // a question the first query did not load: skip, never attach to element 0
			continue
		}
		bank.Questions[i].Options = append(bank.Questions[i].Options, opt)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return domain.Bank{}, err
	}

	rows, err = q.Query(ctx, `
		SELECT qc.question_id, k.name, c.name
		FROM question_categories qc
		JOIN questions qu ON qu.id = qc.question_id
		JOIN exam_categories c ON c.id = qc.category_id
		JOIN exam_classifications k ON k.id = c.classification_id
		WHERE qu.exam_id = $1 AND qu.deleted_at IS NULL AND qc.deleted_at IS NULL AND c.deleted_at IS NULL AND k.deleted_at IS NULL
		ORDER BY qc.id`, examID)
	if err != nil {
		return domain.Bank{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var ref domain.CategoryRef
		if err := rows.Scan(&id, &ref.Classification, &ref.Category); err != nil {
			return domain.Bank{}, err
		}
		i, ok := index[id]
		if !ok {
			continue
		}
		bank.Questions[i].Categories = append(bank.Questions[i].Categories, ref)
	}
	return bank, rows.Err()
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ApplyImport holds a row lock on the exam for the whole transaction, so two
// imports of one exam run one after the other and each plans against the
// state the previous one committed.
func (r *QuestionBankRepository) ApplyImport(ctx context.Context, examID string, plan func(domain.Bank) (*domain.ImportWrite, error)) (err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	var locked string
	if err = tx.QueryRow(ctx, `SELECT id FROM exams WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, examID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = domain.ErrNotFound
		}
		return err
	}
	bank, err := loadBank(ctx, tx, examID)
	if err != nil {
		return err
	}
	write, err := plan(bank)
	if err != nil {
		return err
	}
	if write == nil {
		err = tx.Rollback(ctx)
		return err
	}
	if err = writeImport(ctx, tx, bank, *write); err != nil {
		err = importWriteError(err)
		return err
	}
	return tx.Commit(ctx)
}

// writeImport executes slice 03 §1.6 in order: classifications and
// categories, then questions with options and links, then the weights.
func writeImport(ctx context.Context, tx pgx.Tx, bank domain.Bank, w domain.ImportWrite) error {
	batch := &pgx.Batch{}
	// The image rows go first and commit with the import; a hash that is
	// already stored is left as it is (the bytes are the same by definition).
	for _, f := range w.Media {
		batch.Queue(`INSERT INTO media (sha256, content_type, bytes, width, height, created_at)
			VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (sha256) DO NOTHING`,
			f.Media.SHA256, f.Media.ContentType, f.Media.Bytes, f.Media.Width, f.Media.Height, w.Now)
	}
	for _, c := range w.Classifications {
		batch.Queue(`INSERT INTO exam_classifications (id, exam_id, name, fold_name, position, weight_mode, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, c.ID, w.ExamID, c.Name, c.Fold, c.Position, string(c.WeightMode), w.Now)
	}
	if w.PromoteToPrimary != "" {
		batch.Queue(`UPDATE exam_classifications SET position = 0, weight_mode = 'inferred'
			WHERE id = $1 AND exam_id = $2 AND deleted_at IS NULL`, w.PromoteToPrimary, w.ExamID)
	}
	for _, c := range w.Categories {
		batch.Queue(`INSERT INTO exam_categories (id, classification_id, key, name, fold_name, weight_percent, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, c.ID, c.ClassificationID, c.Key, c.Name, c.Fold, c.WeightPercent, w.Now)
	}

	linksBefore := alivePairs(bank)
	staleChecks := []int{} // batch positions of the version-guarded updates
	for _, c := range w.Creates {
		q := c.Question
		batch.Queue(`INSERT INTO questions (id, exam_id, key, type, stem, explanation, difficulty, source_kind, source_name, origin,
				stem_fingerprint, version, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
			q.ID, w.ExamID, q.Key, string(q.Type), q.Stem, nullable(q.Explanation), nullable(q.Difficulty), q.Source.Kind, q.Source.Name,
			originValue(q.Origin), q.Fingerprint, q.Version, q.CreatedAt, q.UpdatedAt)
		queueOptions(batch, q)
		for _, id := range c.CategoryIDs {
			batch.Queue(`INSERT INTO question_categories (question_id, category_id, created_at) VALUES ($1, $2, $3)`, q.ID, id, w.Now)
		}
	}
	for _, u := range w.Updates {
		q := u.Question
		staleChecks = append(staleChecks, batch.Len())
		batch.Queue(`UPDATE questions SET type = $3, stem = $4, explanation = $5, difficulty = $6, source_kind = $7, source_name = $8,
				origin = $9, stem_fingerprint = $10, version = $11, updated_at = $12
			WHERE id = $1 AND exam_id = $2 AND version = $13 AND deleted_at IS NULL`,
			q.ID, w.ExamID, string(q.Type), q.Stem, nullable(q.Explanation), nullable(q.Difficulty), q.Source.Kind, q.Source.Name,
			originValue(q.Origin), q.Fingerprint, q.Version, q.UpdatedAt, u.ExpectedVersion)
		batch.Queue(`UPDATE question_options SET deleted_at = $2 WHERE question_id = $1 AND deleted_at IS NULL`, q.ID, w.Now)
		queueOptions(batch, q)

		want := map[string]bool{}
		for _, id := range u.CategoryIDs {
			want[id] = true
		}
		for id := range linksBefore[q.ID] {
			if !want[id] {
				batch.Queue(`UPDATE question_categories SET deleted_at = $3 WHERE question_id = $1 AND category_id = $2 AND deleted_at IS NULL`, q.ID, id, w.Now)
			}
		}
		for _, id := range u.CategoryIDs {
			if !linksBefore[q.ID][id] {
				batch.Queue(`INSERT INTO question_categories (question_id, category_id, created_at) VALUES ($1, $2, $3)`, q.ID, id, w.Now)
			}
		}
	}
	for _, cw := range w.Weights {
		batch.Queue(`UPDATE exam_categories SET weight_percent = $2 WHERE id = $1 AND deleted_at IS NULL`, cw.CategoryID, cw.Percent)
	}

	results := tx.SendBatch(ctx, batch)
	stale := map[int]bool{}
	for _, i := range staleChecks {
		stale[i] = true
	}
	for i := 0; i < batch.Len(); i++ {
		tag, err := results.Exec()
		if err != nil {
			_ = results.Close()
			return fmt.Errorf("import statement %d: %w", i, err)
		}
		if stale[i] && tag.RowsAffected() == 0 {
			_ = results.Close()
			return domain.NewConflict("questions", domain.RuleStaleVersion,
				"a question changed while the import was running; run it again")
		}
	}
	return results.Close()
}

// Postgres refuses a NUL in text (22021) and in a jsonb string (22P05); both
// are bad input, so they surface as domain.ErrInvalidText.
const (
	invalidByteSequence     = "22021"
	untranslatableCharacter = "22P05"
)

func importWriteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == invalidByteSequence || pgErr.Code == untranslatableCharacter) {
		return domain.ErrInvalidText
	}
	return err
}

func queueOptions(batch *pgx.Batch, q domain.Question) {
	for pos, o := range q.Options {
		batch.Queue(`INSERT INTO question_options (question_id, position, text, image, alt, correct, rationale)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, q.ID, pos, nullable(o.Text), nullable(o.Image), nullable(o.Alt), o.Correct, nullable(o.Rationale))
	}
}

func originValue(o *domain.Origin) any {
	if o == nil {
		return nil
	}
	b, _ := json.Marshal(originJSON{Set: o.Set, Number: o.Number, NoteRef: o.NoteRef})
	return b
}

// alivePairs maps question id -> set of category ids it is linked to, as of the
// loaded bank. The bank holds names, so ids are resolved through the
// classifications' categories.
func alivePairs(bank domain.Bank) map[string]map[string]bool {
	byName := map[string]string{}
	for _, c := range bank.Classifications {
		for _, k := range c.Categories {
			byName[c.Name+"\x00"+k.Name] = k.ID
		}
	}
	out := map[string]map[string]bool{}
	for _, q := range bank.Questions {
		set := map[string]bool{}
		for _, ref := range q.Categories {
			set[byName[ref.Classification+"\x00"+ref.Category]] = true
		}
		out[q.ID] = set
	}
	return out
}
