package domain

import (
	"slices"
	"testing"
	"time"
)

// Fixtures: every item has options A..D and the answer key B (single) or B+D
// (multi), so a test reads its selections without shuffling.

func keyedSnapshot(multi bool, refs ...CategoryRef) ItemSnapshot {
	s := ItemSnapshot{Key: "k", Type: QuestionSingle, Stem: "stem", Explanation: "why", Categories: refs}
	for _, label := range []string{"A", "B", "C", "D"} {
		s.Options = append(s.Options, SnapshotOption{Label: label, Text: "opt " + label, Rationale: "because " + label,
			Correct: label == "B" || (multi && label == "D")})
	}
	if multi {
		s.Type = QuestionMulti
	}
	return s
}

func scoredItem(position int, selected []string, refs ...CategoryRef) AttemptItem {
	return AttemptItem{Position: position, QuestionID: "q", Snapshot: keyedSnapshot(false, refs...), Selected: selected}
}

func openAttempt(cfg SessionConfig, pass int, items ...AttemptItem) Attempt {
	for i := range items {
		items[i].Position = i
	}
	return Attempt{ID: "att", ExamID: "exam-1", Config: cfg, Status: AttemptInProgress, StartedAt: startedAt, PassPercent: pass, Items: items}
}

var randomCfg = practice(Selection{Kind: SelectRandom, Count: 4})

// ── exact-set correctness (AC-20) ───────────────────────────────────────────

func TestAMultiAnswerIsCorrectOnlyWhenItsSetEqualsTheCorrectSet(t *testing.T) { // AC-20
	s := keyedSnapshot(true)
	for name, tc := range map[string]struct {
		selected []string
		want     bool
	}{
		"B and D":                   {[]string{"B", "D"}, true},
		"D and B, order is moot":    {[]string{"D", "B"}, true},
		"B alone is a subset":       {[]string{"B"}, false},
		"B D E is a superset":       {[]string{"B", "D", "E"}, false},
		"nothing":                   {[]string{}, false},
		"nil":                       {nil, false},
		"the right size, wrong set": {[]string{"A", "B"}, false},
	} {
		if got := s.IsCorrect(tc.selected); got != tc.want {
			t.Errorf("%s: IsCorrect(%v) = %v, want %v", name, tc.selected, got, tc.want)
		}
	}
}

func TestASingleAnswerIsCorrectOnlyWithTheKeyedLabel(t *testing.T) {
	s := keyedSnapshot(false)
	if !s.IsCorrect([]string{"B"}) || s.IsCorrect([]string{"A"}) || s.IsCorrect(nil) || s.IsCorrect([]string{"A", "B"}) {
		t.Error("single correctness is wrong")
	}
}

func TestCorrectLabelsListTheKeyedOptionsInDisplayOrder(t *testing.T) { // S06b-2: correctLabels ["C"]
	s := keyedSnapshot(true)
	if got := s.CorrectLabels(); !slices.Equal(got, []string{"B", "D"}) {
		t.Errorf("CorrectLabels = %v, want [B D]", got)
	}
	s.Options[1].Correct, s.Options[2].Correct = false, true
	if got := s.CorrectLabels(); !slices.Equal(got, []string{"C", "D"}) {
		t.Errorf("CorrectLabels after moving the key = %v, want [C D]", got)
	}
}

// ── the percent (AC-20, S06b-4) ─────────────────────────────────────────────

func TestPercent2RoundsHalfUpToTwoDecimals(t *testing.T) {
	for name, tc := range map[string]struct {
		correct, total int
		want           float64
	}{
		"half":                      {2, 4, 50},
		"a third":                   {1, 3, 33.33},
		"two thirds rounds up":      {2, 3, 66.67},
		"a tie goes up (0.625)":     {1, 160, 0.63},
		"all":                       {200, 200, 100},
		"none":                      {0, 7, 0},
		"an empty attempt scores 0": {0, 0, 0},
		"one in 65":                 {1, 65, 1.54},
		"47 of 65 as a simulation":  {47, 65, 72.31},
	} {
		if got := Percent2(tc.correct, tc.total); got != tc.want {
			t.Errorf("%s: Percent2(%d,%d) = %v, want %v", name, tc.correct, tc.total, got, tc.want)
		}
	}
}

// ── closing and scoring ─────────────────────────────────────────────────────

func TestSubmitScoresFiftyWhenThreeOfFourAreAnsweredAndTwoAreRight(t *testing.T) { // S06b-4
	a := openAttempt(randomCfg, 72,
		scoredItem(0, []string{"B"}), scoredItem(1, []string{"B"}), scoredItem(2, []string{"A"}), scoredItem(3, nil))
	now := startedAt.Add(20 * time.Minute)

	closed := a.Submit(now)

	if closed.Status != AttemptSubmitted || closed.SubmittedAt == nil || !closed.SubmittedAt.Equal(now) {
		t.Errorf("status %q, submittedAt %v; want submitted at %v", closed.Status, closed.SubmittedAt, now)
	}
	if closed.ScorePercent == nil || *closed.ScorePercent != 50 {
		t.Errorf("scorePercent = %v, want 50", closed.ScorePercent)
	}
	var flags []bool
	for _, it := range closed.Items {
		if it.Correct == nil {
			t.Fatalf("item %d has no correct flag after scoring", it.Position)
		}
		flags = append(flags, *it.Correct)
	}
	if !slices.Equal(flags, []bool{true, true, false, false}) {
		t.Errorf("correct flags = %v, want an unanswered item to count as wrong", flags)
	}
}

func TestSubmitDoesNotMutateTheOpenAttempt(t *testing.T) {
	a := openAttempt(randomCfg, 72, scoredItem(0, []string{"B"}))
	_ = a.Submit(startedAt.Add(time.Minute))
	if a.Status != AttemptInProgress || a.ScorePercent != nil || a.SubmittedAt != nil || a.Items[0].Correct != nil {
		t.Errorf("the receiver changed: %+v", a)
	}
}

func TestPassedIsNullForEveryKindButSimulation(t *testing.T) { // S06b-4, FR-10
	items := func() []AttemptItem { return []AttemptItem{scoredItem(0, []string{"B"})} }
	for _, cfg := range []SessionConfig{
		randomCfg,
		practice(Selection{Kind: SelectOriginSet, Set: "s"}),
		practice(Selection{Kind: SelectCategories, ClassificationID: "c", CategoryIDs: []string{"x"}, All: true}),
	} {
		closed := openAttempt(cfg, 72, items()...).Submit(startedAt)
		if closed.Passed != nil {
			t.Errorf("%s: passed = %v, want nil", cfg.Selection.Kind, *closed.Passed)
		}
	}
}

func TestASimulationPassesAtOrAboveThePassMarkFrozenAtTheStart(t *testing.T) { // FR-10, slice 06a §5
	build := func(right, total, pass int) Attempt {
		items := make([]AttemptItem, total)
		for i := range items {
			items[i] = scoredItem(i, []string{"A"})
			if i < right {
				items[i].Selected = []string{"B"}
			}
		}
		return openAttempt(simulation(), pass, items...).Submit(startedAt.Add(time.Hour))
	}
	for name, tc := range map[string]struct {
		right, total, pass int
		want               bool
	}{
		"exactly the pass mark":    {18, 25, 72, true},
		"one below":                {17, 25, 72, false},
		"a perfect score":          {25, 25, 72, true},
		"nothing right":            {0, 25, 72, false},
		"the mark that was frozen": {18, 25, 73, false},
	} {
		closed := build(tc.right, tc.total, tc.pass)
		if closed.Passed == nil || *closed.Passed != tc.want {
			t.Errorf("%s: passed = %v, want %v (score %v, pass %d)", name, closed.Passed, tc.want, closed.ScorePercent, tc.pass)
		}
	}
}

func TestExpireClosesAtTheDeadlineNotAtTheMomentSomeoneLooked(t *testing.T) { // slice 06b §5
	cfg := SessionConfig{Selection: Selection{Kind: SelectRandom, Count: 2}, Timing: &Timing{Kind: Timed, Minutes: 30}, Feedback: FeedbackEnd}
	a := openAttempt(cfg, 72, scoredItem(0, []string{"B"}), scoredItem(1, nil))
	deadline := startedAt.Add(30 * time.Minute)
	a.DeadlineAt = &deadline

	closed := a.Expire()

	if closed.Status != AttemptExpired || closed.SubmittedAt == nil || !closed.SubmittedAt.Equal(deadline) {
		t.Errorf("status %q, submittedAt %v; want expired at the deadline %v", closed.Status, closed.SubmittedAt, deadline)
	}
	if closed.ScorePercent == nil || *closed.ScorePercent != 50 {
		t.Errorf("scorePercent = %v, want 50: whatever was answered is scored", closed.ScorePercent)
	}
	if got := closed.Result().DurationSeconds; got != 1800 {
		t.Errorf("durationSeconds = %d, want 1800", got)
	}
}

func TestAnExpiredSimulationIsStillJudgedAgainstThePassMark(t *testing.T) {
	deadline := startedAt.Add(130 * time.Minute)
	a := openAttempt(simulation(), 50, scoredItem(0, []string{"B"}), scoredItem(1, nil))
	a.DeadlineAt = &deadline
	closed := a.Expire()
	if closed.Passed == nil || !*closed.Passed {
		t.Errorf("passed = %v, want true at 50%% with a 50%% mark", closed.Passed)
	}
}

// ── the result and its breakdown (S06b-5) ───────────────────────────────────

func TestResultCountsDurationInWholeSecondsRoundedDown(t *testing.T) {
	a := openAttempt(randomCfg, 72, scoredItem(0, []string{"B"}), scoredItem(1, []string{"A"}), scoredItem(2, nil))

	r := a.Submit(startedAt.Add(90*time.Second + 900*time.Millisecond)).Result()

	if r.ScorePercent != 33.33 || r.CorrectCount != 1 || r.Total != 3 || r.DurationSeconds != 90 || r.Passed != nil {
		t.Errorf("result = %+v, want 33.33, 1/3, 90 s, no passed", r)
	}
}

func TestBreakdownListsTheWeakestCategoryFirst(t *testing.T) { // S06b-5
	a := openAttempt(randomCfg, 72,
		scoredItem(0, []string{"B"}, dom("A")), scoredItem(1, []string{"B"}, dom("A")),
		scoredItem(2, []string{"C"}, dom("B")), scoredItem(3, nil, dom("B")))

	r := a.Submit(startedAt.Add(time.Minute)).Result()

	if len(r.Breakdown) != 1 || r.Breakdown[0].Classification != "Dominio" {
		t.Fatalf("breakdown = %+v", r.Breakdown)
	}
	want := []CategoryScore{{Name: "B", Correct: 0, Total: 2, Percent: 0}, {Name: "A", Correct: 2, Total: 2, Percent: 100}}
	if got := r.Breakdown[0].Categories; !slices.Equal(got, want) {
		t.Errorf("categories = %+v, want %+v", got, want)
	}
}

func TestBreakdownBreaksPercentTiesByNameAndRoundsThePercent(t *testing.T) {
	a := openAttempt(randomCfg, 72,
		scoredItem(0, []string{"B"}, dom("zeta")), scoredItem(1, []string{"B"}, dom("Alfa")), scoredItem(2, []string{"B"}, dom("beta")),
		scoredItem(3, []string{"A"}, dom("Gamma")), scoredItem(4, []string{"B"}, dom("Gamma")), scoredItem(5, []string{"A"}, dom("Gamma")))

	breakdown := a.Submit(startedAt).Result().Breakdown
	if len(breakdown) != 1 {
		t.Fatalf("breakdown = %+v, want one classification", breakdown)
	}
	cats := breakdown[0].Categories
	var names []string
	for _, c := range cats {
		names = append(names, c.Name)
	}
	if !slices.Equal(names, []string{"Gamma", "Alfa", "beta", "zeta"}) {
		t.Errorf("order = %v, want Gamma (33.33) first, then the 100%% ones by name, case-insensitively", names)
	}
	if len(cats) == 0 || cats[0].Percent != 33.33 || cats[0].Correct != 1 || cats[0].Total != 3 {
		t.Errorf("Gamma = %+v, want 1/3 and 33.33", cats[0])
	}
}

func TestBreakdownGroupsByClassificationSortedByNameAndSkipsUncategorisedItems(t *testing.T) {
	a := openAttempt(randomCfg, 72,
		scoredItem(0, []string{"B"}, svc("VPC"), dom("Seguridad")),
		scoredItem(1, []string{"A"}, dom("Seguridad")),
		scoredItem(2, []string{"B"}), // no category at all
		scoredItem(3, []string{"B"}, svc("Lambda")))

	r := a.Submit(startedAt).Result()

	if r.Total != 4 || r.CorrectCount != 3 {
		t.Errorf("total %d, correct %d; the uncategorised item still counts in the score", r.Total, r.CorrectCount)
	}
	want := []ClassificationScore{
		{Classification: "Dominio", Categories: []CategoryScore{{Name: "Seguridad", Correct: 1, Total: 2, Percent: 50}}},
		// Lambda before VPC: both are 100%, so the name decides.
		{Classification: "Servicio", Categories: []CategoryScore{{Name: "Lambda", Correct: 1, Total: 1, Percent: 100}, {Name: "VPC", Correct: 1, Total: 1, Percent: 100}}},
	}
	if len(r.Breakdown) != 2 {
		t.Fatalf("breakdown = %+v, want %+v", r.Breakdown, want)
	}
	for i := range want {
		if r.Breakdown[i].Classification != want[i].Classification || !slices.Equal(r.Breakdown[i].Categories, want[i].Categories) {
			t.Errorf("breakdown[%d] = %+v, want %+v", i, r.Breakdown[i], want[i])
		}
	}
}

func TestResultOfAttemptWithoutCategoriesHasAnEmptyNotNilBreakdown(t *testing.T) {
	r := openAttempt(randomCfg, 72, scoredItem(0, []string{"B"})).Submit(startedAt).Result()
	if r.Breakdown == nil || len(r.Breakdown) != 0 {
		t.Errorf("breakdown = %#v, want an empty non-nil slice (the wire shows [])", r.Breakdown)
	}
}

// ── the note link (S06b-6) ──────────────────────────────────────────────────

func TestNoteTitleFromRefStripsATrailingSlideReference(t *testing.T) {
	for ref, want := range map[string]string{
		"03 · Route 53 (diap. 12)":      "03 · Route 53",
		"03 · Route 53":                 "03 · Route 53",
		"03 · Route 53  (diap. 12-14) ": "03 · Route 53",
		"  03 · Route 53 ":              "03 · Route 53",
		"Notas (diap. 3) de VPC":        "Notas (diap. 3) de VPC", // only a trailing one
		"Notas (varios)":                "Notas (varios)",
		"(diap. 3)":                     "",
		"":                              "",
	} {
		if got := NoteTitleFromRef(ref); got != want {
			t.Errorf("NoteTitleFromRef(%q) = %q, want %q", ref, got, want)
		}
	}
}
