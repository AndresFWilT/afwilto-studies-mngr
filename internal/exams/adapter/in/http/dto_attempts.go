package http

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
	"github.com/afwilto/studies-mngr/internal/platform/httpx"
)

// ── requests ────────────────────────────────────────────────────────────────

// previewSessionRequest is the contract's SessionPreviewRequest.
type previewSessionRequest struct {
	Config *sessionConfigRequest `json:"config"`
}

// startAttemptRequest is the contract's StartAttemptRequest. Seed stays raw so
// a value outside uint64 is a seed/range violation, not a generic JSON error.
type startAttemptRequest struct {
	Config *sessionConfigRequest `json:"config"`
	Seed   json.RawMessage       `json:"seed"`
}

// sessionConfigRequest mirrors the contract's SessionConfig with pointers, so
// a missing field is "required" rather than a misleading zero.
type sessionConfigRequest struct {
	Selection *selectionRequest `json:"selection"`
	Timing    *timingRequest    `json:"timing"`
	Feedback  *string           `json:"feedback"`
}

type selectionRequest struct {
	Kind             *string         `json:"kind"`
	Count            json.RawMessage `json:"count"` // a number, or "all" for categories
	ClassificationID *string         `json:"classificationId"`
	CategoryIDs      []string        `json:"categoryIds"`
	Set              *string         `json:"set"`
}

type timingRequest struct {
	Kind    *string `json:"kind"`
	Minutes *int    `json:"minutes"`
}

func format(field, message string) domain.Violation {
	return domain.Violation{Field: field, Rule: "format", Message: message}
}

// isAbsent reports a missing or null raw JSON value.
func isAbsent(raw json.RawMessage) bool { return len(raw) == 0 || string(raw) == "null" }

// toDomain maps the wire config to the domain one and reports the problems
// that are about the shape: a missing or malformed field, an id that is not a
// UUID. Business rules (ranges, locked_by_simulation, unknown ids) are the
// domain's, and come back through the use case.
func (c *sessionConfigRequest) toDomain() (domain.SessionConfig, []domain.Violation) {
	if c == nil {
		return domain.SessionConfig{}, []domain.Violation{required("config")}
	}
	var cfg domain.SessionConfig
	sel := c.Selection
	if sel == nil {
		return cfg, []domain.Violation{required("config.selection")}
	}
	if sel.Kind == nil {
		return cfg, []domain.Violation{required("config.selection.kind")}
	}
	var bad []domain.Violation
	cfg.Selection.Kind = domain.SelectionKind(*sel.Kind)

	switch cfg.Selection.Kind {
	case domain.SelectRandom, domain.SelectCategories:
		allowAll := cfg.Selection.Kind == domain.SelectCategories
		switch {
		case isAbsent(sel.Count):
			bad = append(bad, required("config.selection.count"))
		case allowAll && string(sel.Count) == `"all"`:
			cfg.Selection.All = true
		default:
			n, err := strconv.Atoi(string(sel.Count))
			if err != nil {
				msg := "count must be a whole number from 1 to 200"
				if allowAll {
					msg += ` or "all"`
				}
				bad = append(bad, format("config.selection.count", msg))
			}
			cfg.Selection.Count = n
		}
	}
	if cfg.Selection.Kind == domain.SelectCategories {
		if sel.ClassificationID != nil {
			if httpx.IsUUID(*sel.ClassificationID) {
				cfg.Selection.ClassificationID = lowerID(*sel.ClassificationID)
			} else {
				bad = append(bad, format("config.selection.classificationId", "classificationId must be a classification id"))
			}
		}
		for i, id := range sel.CategoryIDs {
			if httpx.IsUUID(id) {
				cfg.Selection.CategoryIDs = append(cfg.Selection.CategoryIDs, lowerID(id))
			} else {
				bad = append(bad, format(fmt.Sprintf("config.selection.categoryIds[%d]", i), "category ids must be category ids"))
			}
		}
	}
	if cfg.Selection.Kind == domain.SelectOriginSet && sel.Set != nil {
		cfg.Selection.Set = *sel.Set
	}

	if t := c.Timing; t != nil {
		if t.Kind == nil {
			bad = append(bad, required("config.timing.kind"))
		} else {
			cfg.Timing = &domain.Timing{Kind: domain.TimingKind(*t.Kind)}
			if cfg.Timing.Kind == domain.Timed {
				if t.Minutes == nil {
					bad = append(bad, required("config.timing.minutes"))
				} else {
					cfg.Timing.Minutes = *t.Minutes
				}
			}
		}
	}
	if c.Feedback != nil {
		cfg.Feedback = domain.Feedback(*c.Feedback)
	}
	return cfg, bad
}

// parseSeed reads the optional seed: absent or null is nil; otherwise a whole
// number from 0 to 2^64-1.
func parseSeed(raw json.RawMessage) (*uint64, []domain.Violation) {
	if isAbsent(raw) {
		return nil, nil
	}
	seed, err := strconv.ParseUint(string(raw), 10, 64)
	if err != nil {
		return nil, []domain.Violation{{Field: "seed", Rule: domain.RuleRange, Message: "seed must be a whole number from 0 to 18446744073709551615"}}
	}
	return &seed, nil
}

// ── responses ───────────────────────────────────────────────────────────────

type shortageDTO struct {
	Category  string `json:"category"`
	Needed    int    `json:"needed"`
	Available int    `json:"available"`
}

// sessionPreviewDTO is the contract's SessionPreview.
type sessionPreviewDTO struct {
	PoolSize         int           `json:"poolSize"`
	DrawCount        int           `json:"drawCount"`
	SuggestedMinutes int           `json:"suggestedMinutes"`
	Shortages        []shortageDTO `json:"shortages"`
}

func toSessionPreviewDTO(p domain.SessionPreview) sessionPreviewDTO {
	dto := sessionPreviewDTO{PoolSize: p.PoolSize, DrawCount: p.DrawCount, SuggestedMinutes: p.SuggestedMinutes, Shortages: make([]shortageDTO, len(p.Shortages))}
	for i, s := range p.Shortages {
		dto.Shortages[i] = shortageDTO{Category: s.Category, Needed: s.Needed, Available: s.Available}
	}
	return dto
}

// sessionConfigDTO is the contract's SessionConfig as echoed back.
type sessionConfigDTO struct {
	Selection selectionDTO `json:"selection"`
	Timing    *timingDTO   `json:"timing,omitempty"`
	Feedback  string       `json:"feedback,omitempty"`
}

type selectionDTO struct {
	Kind             string   `json:"kind"`
	Count            any      `json:"count,omitempty"` // a number, or "all"
	ClassificationID string   `json:"classificationId,omitempty"`
	CategoryIDs      []string `json:"categoryIds,omitempty"`
	Set              string   `json:"set,omitempty"`
}

type timingDTO struct {
	Kind    string `json:"kind"`
	Minutes int    `json:"minutes,omitempty"`
}

func toSessionConfigDTO(c domain.SessionConfig) sessionConfigDTO {
	sel := c.Selection
	dto := sessionConfigDTO{
		Selection: selectionDTO{Kind: string(sel.Kind), ClassificationID: sel.ClassificationID, CategoryIDs: sel.CategoryIDs, Set: sel.Set},
		Feedback:  string(c.Feedback),
	}
	switch {
	case sel.All:
		dto.Selection.Count = "all"
	case sel.Count != 0:
		dto.Selection.Count = sel.Count
	}
	if c.Timing != nil {
		dto.Timing = &timingDTO{Kind: string(c.Timing.Kind), Minutes: c.Timing.Minutes}
	}
	return dto
}

// attemptOptionDTO is the contract's AttemptOption: what is shown, never
// whether it is right.
type attemptOptionDTO struct {
	Label string `json:"label"`
	Text  string `json:"text,omitempty"`
	Image string `json:"image,omitempty"`
	Alt   string `json:"alt,omitempty"`
}

// revealDTO is the answer key of one item: correctness, correct labels,
// rationales by label and the explanation (null when absent). It is embedded
// by pointer and left nil everywhere the key must stay hidden, so omitting it
// is the default and showing it takes an explicit decision.
type revealDTO struct {
	Correct       bool              `json:"correct"`
	CorrectLabels []string          `json:"correctLabels"`
	Rationales    map[string]string `json:"rationales"`
	Explanation   *string           `json:"explanation"`
}

func toRevealDTO(r domain.Reveal) *revealDTO {
	dto := &revealDTO{Correct: r.Correct, CorrectLabels: r.CorrectLabels, Rationales: r.Rationales}
	if dto.CorrectLabels == nil {
		dto.CorrectLabels = []string{}
	}
	if dto.Rationales == nil {
		dto.Rationales = map[string]string{}
	}
	if r.Explanation != "" {
		explanation := r.Explanation
		dto.Explanation = &explanation
	}
	return dto
}

func orEmpty(labels []string) []string {
	if labels == nil {
		return []string{}
	}
	return labels
}

func toOptionDTOs(options []domain.SnapshotOption) []attemptOptionDTO {
	out := make([]attemptOptionDTO, len(options))
	for i, o := range options {
		out[i] = attemptOptionDTO{Label: o.Label, Text: o.Text, Image: o.Image, Alt: o.Alt}
	}
	return out
}

// attemptItemDTO is the contract's AttemptItem. It is built field by field from
// the snapshot and never serializes the snapshot itself, so the answer key
// cannot reach the wire by adding a field there. The reveal is set only for
// items revealed under feedback each.
type attemptItemDTO struct {
	Position    int                `json:"position"`
	Stem        string             `json:"stem"`
	Type        string             `json:"type"`
	ChooseCount *int               `json:"chooseCount,omitempty"`
	Options     []attemptOptionDTO `json:"options"`
	Selected    []string           `json:"selected"`
	Flagged     bool               `json:"flagged"`
	Answered    bool               `json:"answered"`
	*revealDTO
}

// attemptDTO is the contract's Attempt.
type attemptDTO struct {
	ID               string           `json:"id"`
	ExamID           string           `json:"examId"`
	Config           sessionConfigDTO `json:"config"`
	Status           string           `json:"status"`
	StartedAt        time.Time        `json:"startedAt"`
	DeadlineAt       *time.Time       `json:"deadlineAt,omitempty"`
	RemainingSeconds *int             `json:"remainingSeconds,omitempty"`
	SuggestedMinutes int              `json:"suggestedMinutes"`
	Items            []attemptItemDTO `json:"items"`
	Result           *resultDTO       `json:"result,omitempty"`
}

func toAttemptDTO(v domain.AttemptView) attemptDTO {
	dto := attemptDTO{
		ID: v.ID, ExamID: v.ExamID, Config: toSessionConfigDTO(v.Config), Status: string(v.Status),
		StartedAt: v.StartedAt.UTC(), RemainingSeconds: v.RemainingSeconds, SuggestedMinutes: v.SuggestedMinutes,
		Items: make([]attemptItemDTO, len(v.Items)),
	}
	if v.DeadlineAt != nil {
		deadline := v.DeadlineAt.UTC()
		dto.DeadlineAt = &deadline
	}
	if v.Result != nil {
		result := toResultDTO(*v.Result)
		dto.Result = &result
	}
	for i, it := range v.Items {
		item := attemptItemDTO{
			Position: it.Position, Stem: it.Snapshot.Stem, Type: string(it.Snapshot.Type), Options: toOptionDTOs(it.Snapshot.Options),
			Selected: orEmpty(it.Selected), Flagged: it.Flagged, Answered: len(it.Selected) > 0,
		}
		if n := it.Snapshot.ChooseCount(); n > 0 {
			item.ChooseCount = &n
		}
		if it.Revealed {
			item.revealDTO = toRevealDTO(it.Reveal())
		}
		dto.Items[i] = item
	}
	return dto
}

// ── answer ──────────────────────────────────────────────────────────────────

// answerRequest is the contract's AnswerRequest; both fields are pointers so a
// missing or null one is "required" and not a silent zero.
type answerRequest struct {
	Selected *[]string `json:"selected"`
	Flagged  *bool     `json:"flagged"`
}

func (r answerRequest) toDomain() (domain.AnswerInput, []domain.Violation) {
	var bad []domain.Violation
	if r.Selected == nil {
		bad = append(bad, required("selected"))
	}
	if r.Flagged == nil {
		bad = append(bad, required("flagged"))
	}
	if len(bad) > 0 {
		return domain.AnswerInput{}, bad
	}
	return domain.AnswerInput{Selected: *r.Selected, Flagged: *r.Flagged}, nil
}

// answerDTO is the data of the contract's AnswerResponse. With feedback end it
// holds the position, the selection and the flag, plus the server clock on
// timed attempts: nothing about correctness (BR-A4).
type answerDTO struct {
	Position         int      `json:"position"`
	Selected         []string `json:"selected"`
	Flagged          bool     `json:"flagged"`
	RemainingSeconds *int     `json:"remainingSeconds,omitempty"`
	*revealDTO
}

func toAnswerDTO(o domain.AnswerOutcome) answerDTO {
	dto := answerDTO{Position: o.Position, Selected: orEmpty(o.Selected), Flagged: o.Flagged, RemainingSeconds: o.RemainingSeconds}
	if o.Reveal != nil {
		dto.revealDTO = toRevealDTO(*o.Reveal)
	}
	return dto
}

// ── result ──────────────────────────────────────────────────────────────────

type categoryScoreDTO struct {
	Name    string  `json:"name"`
	Correct int     `json:"correct"`
	Total   int     `json:"total"`
	Percent float64 `json:"percent"`
}

type classificationScoreDTO struct {
	Classification string             `json:"classification"`
	Categories     []categoryScoreDTO `json:"categories"`
}

// resultDTO is the contract's AttemptResult; passed is an explicit null for
// anything but a simulation.
type resultDTO struct {
	ScorePercent    float64                  `json:"scorePercent"`
	Passed          *bool                    `json:"passed"`
	CorrectCount    int                      `json:"correctCount"`
	Total           int                      `json:"total"`
	DurationSeconds int                      `json:"durationSeconds"`
	Breakdown       []classificationScoreDTO `json:"breakdown"`
}

func toResultDTO(r domain.AttemptResult) resultDTO {
	dto := resultDTO{
		ScorePercent: r.ScorePercent, Passed: r.Passed, CorrectCount: r.CorrectCount, Total: r.Total, DurationSeconds: r.DurationSeconds,
		Breakdown: make([]classificationScoreDTO, len(r.Breakdown)),
	}
	for i, c := range r.Breakdown {
		cats := make([]categoryScoreDTO, len(c.Categories))
		for j, s := range c.Categories {
			cats[j] = categoryScoreDTO{Name: s.Name, Correct: s.Correct, Total: s.Total, Percent: s.Percent}
		}
		dto.Breakdown[i] = classificationScoreDTO{Classification: c.Classification, Categories: cats}
	}
	return dto
}

// ── review ──────────────────────────────────────────────────────────────────

type reviewCategoryDTO struct {
	Classification string `json:"classification"`
	Name           string `json:"name"`
}

// reviewItemDTO is the contract's ReviewItem.
type reviewItemDTO struct {
	Position   int                 `json:"position"`
	Stem       string              `json:"stem"`
	Type       string              `json:"type"`
	Options    []attemptOptionDTO  `json:"options"`
	Selected   []string            `json:"selected"`
	Flagged    bool                `json:"flagged"`
	Categories []reviewCategoryDTO `json:"categories"`
	NoteID     string              `json:"noteId,omitempty"`
	*revealDTO
}

func toReviewItemDTOs(items []domain.ReviewItem) []reviewItemDTO {
	out := make([]reviewItemDTO, len(items))
	for i, r := range items {
		it := r.Item
		dto := reviewItemDTO{
			Position: it.Position, Stem: it.Snapshot.Stem, Type: string(it.Snapshot.Type), Options: toOptionDTOs(it.Snapshot.Options),
			Selected: orEmpty(it.Selected), Flagged: it.Flagged, Categories: make([]reviewCategoryDTO, len(it.Snapshot.Categories)),
			NoteID: r.NoteID, revealDTO: toRevealDTO(r.Reveal),
		}
		for j, c := range it.Snapshot.Categories {
			dto.Categories[j] = reviewCategoryDTO{Classification: c.Classification, Name: c.Category}
		}
		out[i] = dto
	}
	return out
}

// ── history ─────────────────────────────────────────────────────────────────

// attemptSummaryDTO is the contract's AttemptSummary; the outcome fields are
// absent while the attempt is open and passed is absent unless the attempt is a
// simulation.
type attemptSummaryDTO struct {
	ID              string    `json:"id"`
	SelectionKind   string    `json:"selectionKind"`
	TimingKind      string    `json:"timingKind"`
	Feedback        string    `json:"feedback"`
	Status          string    `json:"status"`
	StartedAt       time.Time `json:"startedAt"`
	DurationSeconds *int      `json:"durationSeconds,omitempty"`
	ScorePercent    *float64  `json:"scorePercent,omitempty"`
	Passed          *bool     `json:"passed,omitempty"`
}

type trendPointDTO struct {
	At           time.Time `json:"at"`
	ScorePercent float64   `json:"scorePercent"`
}

type historyMetaDTO struct {
	TotalItems int             `json:"totalItems"`
	Trend      []trendPointDTO `json:"trend"`
}

func toHistoryDTO(h domain.AttemptHistory) ([]attemptSummaryDTO, historyMetaDTO) {
	rows := make([]attemptSummaryDTO, len(h.Attempts))
	for i, s := range h.Attempts {
		rows[i] = attemptSummaryDTO{
			ID: s.ID, SelectionKind: string(s.SelectionKind), TimingKind: string(s.TimingKind), Feedback: string(s.Feedback), Status: string(s.Status),
			StartedAt: s.StartedAt.UTC(), DurationSeconds: s.DurationSeconds, ScorePercent: s.ScorePercent, Passed: s.Passed,
		}
	}
	meta := historyMetaDTO{TotalItems: len(rows), Trend: make([]trendPointDTO, len(h.Trend))}
	for i, p := range h.Trend {
		meta.Trend[i] = trendPointDTO{At: p.At.UTC(), ScorePercent: p.ScorePercent}
	}
	return rows, meta
}
