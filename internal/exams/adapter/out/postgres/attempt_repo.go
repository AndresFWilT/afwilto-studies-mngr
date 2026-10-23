package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/afwilto/studies-mngr/internal/exams/app/port/out"
	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// openAttemptIndex is the partial unique index that enforces BR-A1.
const openAttemptIndex = "attempts_exam_in_progress_ux"

// AttemptRepository persists attempts and their frozen items (migration 0013).
type AttemptRepository struct{ pool *pgxpool.Pool }

func NewAttemptRepository(pool *pgxpool.Pool) *AttemptRepository {
	return &AttemptRepository{pool: pool}
}

// Create stores the attempt and all its items in one transaction. The exam row
// is locked FOR SHARE, so an exam delete cannot slip in between; two starts
// both pass that lock and the partial unique index decides the loser, who then
// learns the id of the winner.
func (r *AttemptRepository) Create(ctx context.Context, a domain.Attempt) error {
	// The winner can close between our conflict and our look at it; a second
	// round then simply wins or loses again.
	for range 3 {
		err := r.insert(ctx, a)
		if !isOpenAttemptViolation(err) {
			return err
		}
		if open, found, lookupErr := r.FindInProgress(ctx, a.ExamID); lookupErr != nil {
			return lookupErr
		} else if found {
			return domain.NewAttemptInProgress(open)
		}
	}
	return fmt.Errorf("creating attempt %s: the open attempt keeps changing", a.ID)
}

func isOpenAttemptViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation && pgErr.ConstraintName == openAttemptIndex
}

func (r *AttemptRepository) insert(ctx context.Context, a domain.Attempt) (err error) {
	config, err := encodeConfig(a.Config)
	if err != nil {
		return err
	}
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
	if err = tx.QueryRow(ctx, `SELECT id FROM exams WHERE id = $1 AND deleted_at IS NULL FOR SHARE`, a.ExamID).Scan(&locked); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			err = domain.ErrNotFound
		}
		return err
	}
	if _, err = tx.Exec(ctx, `
		INSERT INTO attempts (id, exam_id, config, status, seed, started_at, deadline_at, submitted_at, score_percent, passed, suggested_minutes, pass_percent)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		a.ID, a.ExamID, config, string(a.Status), int64(a.Seed), a.StartedAt.UTC(), utcPtr(a.DeadlineAt), utcPtr(a.SubmittedAt),
		a.ScorePercent, a.Passed, a.SuggestedMinutes, a.PassPercent); err != nil {
		return err
	}

	batch := &pgx.Batch{}
	for _, it := range a.Items {
		snapshot, encodeErr := encodeSnapshot(it.Snapshot)
		if encodeErr != nil {
			return encodeErr
		}
		selected := it.Selected
		if selected == nil {
			selected = []string{}
		}
		batch.Queue(`
			INSERT INTO attempt_items (attempt_id, position, question_id, snapshot, selected, flagged, answered_at, revealed, correct)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			a.ID, it.Position, it.QuestionID, snapshot, selected, it.Flagged, utcPtr(it.AnsweredAt), it.Revealed, it.Correct)
	}
	results := tx.SendBatch(ctx, batch)
	for range a.Items {
		if _, err = results.Exec(); err != nil {
			_ = results.Close()
			return err
		}
	}
	if err = results.Close(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func (r *AttemptRepository) FindInProgress(ctx context.Context, examID string) (string, bool, error) {
	var id string
	err := r.pool.QueryRow(ctx, `SELECT id FROM attempts WHERE exam_id = $1 AND status = 'in_progress'`, examID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return id, true, nil
}

// Get reads the attempt and its items in one snapshot, so a concurrent close
// is seen whole or not at all.
func (r *AttemptRepository) Get(ctx context.Context, id string) (a domain.Attempt, err error) {
	err = readSnapshot(ctx, r.pool, func(q queryer) error {
		var loadErr error
		a, loadErr = loadAttempt(ctx, q, id)
		return loadErr
	})
	if err != nil {
		return domain.Attempt{}, err
	}
	return a, nil
}

// Mutate loads the attempt with its header row locked FOR UPDATE, so two
// requests on one attempt (an answer racing a submit, two submits, a read that
// finalizes) run one after the other and each sees the other's result. The
// change plan returns is stored in the same transaction; with an error it is
// stored too, so a refused write can still persist the expiry it found.
func (r *AttemptRepository) Mutate(ctx context.Context, id string, plan func(domain.Attempt) (*out.AttemptChange, error)) (err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after Commit

	a, err := loadAttemptFor(ctx, tx, id, true)
	if err != nil {
		return err
	}
	change, planErr := plan(a)
	if change == nil {
		return planErr // nothing to store; the deferred rollback ends the transaction
	}
	if err := storeChange(ctx, tx, id, *change); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	return planErr
}

// storeChange writes the header fields a close changes and the answer state of
// the listed items.
func storeChange(ctx context.Context, tx pgx.Tx, id string, change out.AttemptChange) error {
	if h := change.Header; h != nil {
		if _, err := tx.Exec(ctx, `
			UPDATE attempts SET status = $2, submitted_at = $3, score_percent = $4, passed = $5 WHERE id = $1`,
			id, string(h.Status), utcPtr(h.SubmittedAt), h.ScorePercent, h.Passed); err != nil {
			return err
		}
	}
	batch := &pgx.Batch{}
	for _, it := range change.Items {
		selected := it.Selected
		if selected == nil {
			selected = []string{}
		}
		batch.Queue(`
			UPDATE attempt_items SET selected = $3, flagged = $4, answered_at = $5, revealed = $6, correct = $7
			WHERE attempt_id = $1 AND position = $2`,
			id, it.Position, selected, it.Flagged, utcPtr(it.AnsweredAt), it.Revealed, it.Correct)
	}
	results := tx.SendBatch(ctx, batch)
	for range change.Items {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return err
		}
	}
	return results.Close()
}

// ListByExam reads the header of every attempt of the exam; the items are not
// needed for a history row.
func (r *AttemptRepository) ListByExam(ctx context.Context, examID string) ([]domain.AttemptSummary, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, config, status, started_at, submitted_at, score_percent, passed
		FROM attempts WHERE exam_id = $1`, examID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := []domain.AttemptSummary{}
	for rows.Next() {
		var (
			a      domain.Attempt
			config []byte
			status string
		)
		if err := rows.Scan(&a.ID, &config, &status, &a.StartedAt, &a.SubmittedAt, &a.ScorePercent, &a.Passed); err != nil {
			return nil, err
		}
		a.Status, a.StartedAt, a.SubmittedAt = domain.AttemptStatus(status), a.StartedAt.UTC(), utcPtr(a.SubmittedAt)
		if a.Config, err = decodeConfig(config); err != nil {
			return nil, fmt.Errorf("attempt %s config: %w", a.ID, err)
		}
		summaries = append(summaries, a.Summary())
	}
	return summaries, rows.Err()
}

// loadAttempt reads an attempt with its items back. domain.ErrNotFound when absent.
func loadAttempt(ctx context.Context, q queryer, id string) (domain.Attempt, error) {
	return loadAttemptFor(ctx, q, id, false)
}

// loadAttemptFor is loadAttempt; with forUpdate it locks the attempt's header
// row for the rest of the transaction.
func loadAttemptFor(ctx context.Context, q queryer, id string, forUpdate bool) (domain.Attempt, error) {
	a := domain.Attempt{ID: id}
	lock := ""
	if forUpdate {
		lock = " FOR UPDATE"
	}
	var (
		config []byte
		status string
		seed   int64
	)
	err := q.QueryRow(ctx, `
		SELECT exam_id, config, status, seed, started_at, deadline_at, submitted_at, score_percent, passed, suggested_minutes, pass_percent
		FROM attempts WHERE id = $1`+lock, id).Scan(
		&a.ExamID, &config, &status, &seed, &a.StartedAt, &a.DeadlineAt, &a.SubmittedAt, &a.ScorePercent, &a.Passed, &a.SuggestedMinutes, &a.PassPercent)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Attempt{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Attempt{}, err
	}
	a.Status, a.Seed = domain.AttemptStatus(status), uint64(seed)
	a.StartedAt = a.StartedAt.UTC()
	a.DeadlineAt, a.SubmittedAt = utcPtr(a.DeadlineAt), utcPtr(a.SubmittedAt)
	if a.Config, err = decodeConfig(config); err != nil {
		return domain.Attempt{}, fmt.Errorf("attempt %s config: %w", id, err)
	}

	rows, err := q.Query(ctx, `
		SELECT position, question_id, snapshot, selected, flagged, answered_at, revealed, correct
		FROM attempt_items WHERE attempt_id = $1 ORDER BY position`, id)
	if err != nil {
		return domain.Attempt{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			it       domain.AttemptItem
			snapshot []byte
		)
		if err := rows.Scan(&it.Position, &it.QuestionID, &snapshot, &it.Selected, &it.Flagged, &it.AnsweredAt, &it.Revealed, &it.Correct); err != nil {
			return domain.Attempt{}, err
		}
		it.AnsweredAt = utcPtr(it.AnsweredAt)
		if it.Snapshot, err = decodeSnapshot(snapshot); err != nil {
			return domain.Attempt{}, fmt.Errorf("attempt %s item %d snapshot: %w", id, it.Position, err)
		}
		a.Items = append(a.Items, it)
	}
	return a, rows.Err()
}

// ── JSON codecs ─────────────────────────────────────────────────────────────

// configJSON is the stored session config, in the wire shape of the contract's
// SessionConfig. The HTTP adapter keeps its own copy of the shape: adapters do
// not import each other.
type configJSON struct {
	Selection selectionJSON `json:"selection"`
	Timing    *timingJSON   `json:"timing,omitempty"`
	Feedback  string        `json:"feedback,omitempty"`
}

type selectionJSON struct {
	Kind             string          `json:"kind"`
	Count            json.RawMessage `json:"count,omitempty"` // a number, or "all" for categories
	ClassificationID string          `json:"classificationId,omitempty"`
	CategoryIDs      []string        `json:"categoryIds,omitempty"`
	Set              string          `json:"set,omitempty"`
}

type timingJSON struct {
	Kind    string `json:"kind"`
	Minutes int    `json:"minutes,omitempty"`
}

func encodeConfig(cfg domain.SessionConfig) ([]byte, error) {
	sel := cfg.Selection
	out := configJSON{
		Selection: selectionJSON{Kind: string(sel.Kind), ClassificationID: sel.ClassificationID, CategoryIDs: sel.CategoryIDs, Set: sel.Set},
		Feedback:  string(cfg.Feedback),
	}
	switch {
	case sel.All:
		out.Selection.Count = json.RawMessage(`"all"`)
	case sel.Count != 0:
		out.Selection.Count = json.RawMessage(fmt.Sprint(sel.Count))
	}
	if cfg.Timing != nil {
		out.Timing = &timingJSON{Kind: string(cfg.Timing.Kind), Minutes: cfg.Timing.Minutes}
	}
	return json.Marshal(out)
}

func decodeConfig(raw []byte) (domain.SessionConfig, error) {
	var in configJSON
	if err := json.Unmarshal(raw, &in); err != nil {
		return domain.SessionConfig{}, err
	}
	cfg := domain.SessionConfig{
		Selection: domain.Selection{Kind: domain.SelectionKind(in.Selection.Kind), ClassificationID: in.Selection.ClassificationID,
			CategoryIDs: in.Selection.CategoryIDs, Set: in.Selection.Set},
		Feedback: domain.Feedback(in.Feedback),
	}
	if len(in.Selection.Count) > 0 {
		if string(in.Selection.Count) == `"all"` {
			cfg.Selection.All = true
		} else if err := json.Unmarshal(in.Selection.Count, &cfg.Selection.Count); err != nil {
			return domain.SessionConfig{}, fmt.Errorf("selection.count: %w", err)
		}
	}
	if in.Timing != nil {
		cfg.Timing = &domain.Timing{Kind: domain.TimingKind(in.Timing.Kind), Minutes: in.Timing.Minutes}
	}
	return cfg, nil
}

// snapshotJSON is the frozen question of an attempt item (attempt_items.snapshot).
type snapshotJSON struct {
	Key         string               `json:"key"`
	Type        string               `json:"type"`
	Stem        string               `json:"stem"`
	Options     []snapshotOptionJSON `json:"options"`
	Explanation string               `json:"explanation,omitempty"`
	Categories  []categoryRefJSON    `json:"categories,omitempty"`
	Origin      *originJSON          `json:"origin,omitempty"`
	PaceMinutes float64              `json:"paceMinutes"`
}

type snapshotOptionJSON struct {
	Label     string `json:"label"`
	Text      string `json:"text,omitempty"`
	Image     string `json:"image,omitempty"`
	Alt       string `json:"alt,omitempty"`
	Correct   bool   `json:"correct"`
	Rationale string `json:"rationale,omitempty"`
}

type categoryRefJSON struct {
	Classification string `json:"classification"`
	Category       string `json:"category"`
}

func encodeSnapshot(s domain.ItemSnapshot) ([]byte, error) {
	out := snapshotJSON{Key: s.Key, Type: string(s.Type), Stem: s.Stem, Explanation: s.Explanation, PaceMinutes: s.PaceMinutes,
		Options: make([]snapshotOptionJSON, len(s.Options))}
	for i, o := range s.Options {
		out.Options[i] = snapshotOptionJSON{Label: o.Label, Text: o.Text, Image: o.Image, Alt: o.Alt, Correct: o.Correct, Rationale: o.Rationale}
	}
	for _, c := range s.Categories {
		out.Categories = append(out.Categories, categoryRefJSON{Classification: c.Classification, Category: c.Category})
	}
	if s.Origin != nil {
		out.Origin = &originJSON{Set: s.Origin.Set, Number: s.Origin.Number, NoteRef: s.Origin.NoteRef}
	}
	return json.Marshal(out)
}

func decodeSnapshot(raw []byte) (domain.ItemSnapshot, error) {
	var in snapshotJSON
	if err := json.Unmarshal(raw, &in); err != nil {
		return domain.ItemSnapshot{}, err
	}
	s := domain.ItemSnapshot{Key: in.Key, Type: domain.QuestionType(in.Type), Stem: in.Stem, Explanation: in.Explanation, PaceMinutes: in.PaceMinutes,
		Options: make([]domain.SnapshotOption, len(in.Options))}
	for i, o := range in.Options {
		s.Options[i] = domain.SnapshotOption{Label: o.Label, Text: o.Text, Image: o.Image, Alt: o.Alt, Correct: o.Correct, Rationale: o.Rationale}
	}
	for _, c := range in.Categories {
		s.Categories = append(s.Categories, domain.CategoryRef{Classification: c.Classification, Category: c.Category})
	}
	if in.Origin != nil {
		s.Origin = &domain.Origin{Set: in.Origin.Set, Number: in.Origin.Number, NoteRef: in.Origin.NoteRef}
	}
	return s, nil
}
