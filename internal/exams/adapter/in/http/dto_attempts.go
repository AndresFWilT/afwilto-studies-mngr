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

// attemptItemDTO is the contract's AttemptItem as the start returns it. It is
// built field by field from the snapshot and never serializes the snapshot
// itself, so the answer key cannot reach the wire by adding a field there.
type attemptItemDTO struct {
	Position    int                `json:"position"`
	Stem        string             `json:"stem"`
	Type        string             `json:"type"`
	ChooseCount *int               `json:"chooseCount,omitempty"`
	Options     []attemptOptionDTO `json:"options"`
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
	for i, it := range v.Items {
		item := attemptItemDTO{Position: it.Position, Stem: it.Snapshot.Stem, Type: string(it.Snapshot.Type), Options: make([]attemptOptionDTO, len(it.Snapshot.Options))}
		if n := it.Snapshot.ChooseCount(); n > 0 {
			item.ChooseCount = &n
		}
		for j, o := range it.Snapshot.Options {
			item.Options[j] = attemptOptionDTO{Label: o.Label, Text: o.Text, Image: o.Image, Alt: o.Alt}
		}
		dto.Items[i] = item
	}
	return dto
}
