package postgres

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

func TestConfigRoundTripsEverySelectionKind(t *testing.T) { // slice 06a §1.1, attempts.config jsonb
	untimed := &domain.Timing{Kind: domain.Untimed}
	timed := &domain.Timing{Kind: domain.Timed, Minutes: 45}
	for name, cfg := range map[string]domain.SessionConfig{
		"simulation": {Selection: domain.Selection{Kind: domain.SelectSimulation}},
		"random":     {Selection: domain.Selection{Kind: domain.SelectRandom, Count: 40}, Timing: untimed, Feedback: domain.FeedbackEnd},
		"categories counted": {Selection: domain.Selection{Kind: domain.SelectCategories, ClassificationID: "cl-1", CategoryIDs: []string{"k-1", "k-2"}, Count: 20},
			Timing: timed, Feedback: domain.FeedbackEach},
		"categories all": {Selection: domain.Selection{Kind: domain.SelectCategories, ClassificationID: "cl-1", CategoryIDs: []string{"k-1"}, All: true},
			Timing: untimed, Feedback: domain.FeedbackEach},
		"origin-set": {Selection: domain.Selection{Kind: domain.SelectOriginSet, Set: "Timed Set 3"}, Timing: timed, Feedback: domain.FeedbackEnd},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := encodeConfig(cfg)
			if err != nil {
				t.Fatal(err)
			}
			got, err := decodeConfig(raw)
			if err != nil || !reflect.DeepEqual(got, cfg) {
				t.Errorf("decode(encode(cfg)) = %+v, %v\nraw %s\nwant %+v", got, err, raw, cfg)
			}
		})
	}
}

func TestConfigIsStoredInTheWireShapeOfTheContract(t *testing.T) { // the stored JSON is what GET returns as config
	cfg := domain.SessionConfig{
		Selection: domain.Selection{Kind: domain.SelectCategories, ClassificationID: "cl-1", CategoryIDs: []string{"k-1"}, All: true},
		Timing:    &domain.Timing{Kind: domain.Timed, Minutes: 30}, Feedback: domain.FeedbackEach,
	}
	raw, _ := encodeConfig(cfg)

	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	want := map[string]any{
		"selection": map[string]any{"kind": "categories", "classificationId": "cl-1", "categoryIds": []any{"k-1"}, "count": "all"},
		"timing":    map[string]any{"kind": "timed", "minutes": float64(30)},
		"feedback":  "each",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("stored config = %s, want %v", raw, want)
	}

	sim, _ := encodeConfig(domain.SessionConfig{Selection: domain.Selection{Kind: domain.SelectSimulation}})
	if string(sim) != `{"selection":{"kind":"simulation"}}` {
		t.Errorf("simulation config = %s, want only the selection (timing and feedback stay omitted)", sim)
	}
}

func TestSnapshotRoundTripsEveryField(t *testing.T) { // slice 06a §1.3 Snapshot, amendment 2026-10-23
	want := domain.ItemSnapshot{
		Key: "vpc-1", Type: domain.QuestionMulti, Stem: "Which **two**?\n\n![Señal](media:" + fakeSHA + ")", Explanation: "Because *roles*.",
		Options: []domain.SnapshotOption{
			{Label: "A", Text: "uno", Correct: true, Rationale: "yes"},
			{Label: "B", Image: "media:" + fakeSHA, Alt: "a diagram"},
			{Label: "C", Text: "tres", Correct: true},
		},
		Categories:  []domain.CategoryRef{{Classification: "Dominio", Category: "Seguridad"}, {Classification: "Servicio", Category: "IAM"}},
		Origin:      &domain.Origin{Set: "Timed Set 3", Number: 7, NoteRef: "03 · Route 53 (diap. 12)"},
		PaceMinutes: 2.5,
	}
	raw, err := encodeSnapshot(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeSnapshot(raw)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("decode(encode(s)) = %+v, %v\nraw %s\nwant %+v", got, err, raw, want)
	}
}

func TestSnapshotWithoutOptionalPartsRoundTrips(t *testing.T) {
	want := domain.ItemSnapshot{
		Key: "q", Type: domain.QuestionSingle, Stem: "s", PaceMinutes: 2,
		Options: []domain.SnapshotOption{{Label: "A", Text: "x", Correct: true}, {Label: "B", Text: "y"}},
	}
	raw, _ := encodeSnapshot(want)
	got, err := decodeSnapshot(raw)
	if err != nil || got.Origin != nil || got.Explanation != "" || len(got.Categories) != 0 || got.PaceMinutes != 2 {
		t.Errorf("decoded = %+v, %v (raw %s)", got, err, raw)
	}
}

const fakeSHA = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
