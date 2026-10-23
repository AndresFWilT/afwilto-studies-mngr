package usecase

import (
	"testing"
	"time"

	"github.com/afwilto/studies-mngr/internal/exams/domain"
)

// answerRight answers the first n items with their key and the next m with a
// wrong label; the rest stay unanswered.
func (e sessionEnv) answerMix(t *testing.T, id string, right, wrong int) {
	t.Helper()
	for i := 0; i < right; i++ {
		e.save(t, id, i, false, e.keyOf(id, i)...)
	}
	for i := right; i < right+wrong; i++ {
		e.save(t, id, i, false, e.wrongOf(id, i))
	}
}

func TestSubmitScoresFiftyPercentAndLeavesPassedNullOnARandomAttempt(t *testing.T) { // S06b-4
	e := newSessionEnv(t)
	view := e.begin(t, randomUntimed(4), nil)
	e.answerMix(t, view.ID, 2, 1) // 3 answered, 2 right, 1 unanswered
	e.at(7 * time.Minute)

	r, err := e.submit.Execute(ctx, view.ID)

	if err != nil {
		t.Fatal(err)
	}
	if r.ScorePercent != 50 || r.Passed != nil || r.CorrectCount != 2 || r.Total != 4 || r.DurationSeconds != 420 {
		t.Errorf("result = %+v, want 50%%, no passed, 2/4, 420 s", r)
	}
	stored := e.attempts.Rows[view.ID]
	if stored.Status != domain.AttemptSubmitted || stored.ScorePercent == nil || *stored.ScorePercent != 50 || stored.Passed != nil ||
		stored.SubmittedAt == nil || !stored.SubmittedAt.Equal(t0.Add(7*time.Minute)) {
		t.Errorf("stored = %q, score %v, passed %v, submittedAt %v", stored.Status, stored.ScorePercent, stored.Passed, stored.SubmittedAt)
	}
	for _, it := range stored.Items {
		if it.Correct == nil {
			t.Errorf("item %d has no stored correctness after the score", it.Position)
		}
	}
}

func TestSubmitOfASimulationSetsPassedAgainstThePassMarkFrozenAtTheStart(t *testing.T) { // FR-10: 72%
	for name, tc := range map[string]struct {
		right int
		want  bool
		score float64
	}{"47 of 65 passes": {47, true, 72.31}, "46 of 65 fails": {46, false, 70.77}} {
		e := newSessionEnv(t)
		view := e.begin(t, domain.SessionConfig{Selection: domain.Selection{Kind: domain.SelectSimulation}}, nil)
		e.answerMix(t, view.ID, tc.right, 0)

		r, err := e.submit.Execute(ctx, view.ID)

		if err != nil || r.Passed == nil || *r.Passed != tc.want || r.ScorePercent != tc.score {
			t.Errorf("%s: result %+v (err %v), want passed %v at %v", name, r, err, tc.want, tc.score)
		}
	}
}

func TestSubmitUsesThePassMarkOfTheStartNotTheExamsCurrentOne(t *testing.T) { // slice 06a §5
	e := newSessionEnv(t)
	view := e.begin(t, domain.SessionConfig{Selection: domain.Selection{Kind: domain.SelectSimulation}}, nil)
	e.answerMix(t, view.ID, 47, 0) // 72.31%
	exam, _ := e.repo.Get(ctx, e.examID)
	exam.PassPercent = 90
	if err := e.repo.Update(ctx, exam, exam.Version); err != nil {
		t.Fatal(err)
	}

	r, err := e.submit.Execute(ctx, view.ID)

	if err != nil || r.Passed == nil || !*r.Passed {
		t.Errorf("result %+v (err %v), want passed against the frozen 72, not the edited 90", r, err)
	}
}

func TestSubmitReturnsTheBreakdownWeakestCategoryFirst(t *testing.T) { // S06b-5
	e := newSessionEnv(t)
	view := e.begin(t, randomUntimed(20), nil)
	stored := e.attempts.Rows[view.ID]
	// Answer right exactly the Costos items; every Seguridad item stays wrong.
	for _, it := range stored.Items {
		if it.Snapshot.Categories[0].Category == "Costos" {
			e.save(t, view.ID, it.Position, false, e.keyOf(view.ID, it.Position)...)
		}
	}

	r, err := e.submit.Execute(ctx, view.ID)

	if err != nil || len(r.Breakdown) != 1 {
		t.Fatalf("result %+v (err %v)", r, err)
	}
	cats := r.Breakdown[0].Categories
	if r.Breakdown[0].Classification != "Dominio" || len(cats) != 2 || cats[0].Name != "Seguridad" || cats[0].Percent != 0 || cats[1].Name != "Costos" || cats[1].Percent != 100 {
		t.Errorf("breakdown = %+v, want Seguridad (0%%) before Costos (100%%)", r.Breakdown)
	}
}

func TestASecondSubmitIsAttemptClosed(t *testing.T) {
	e := newSessionEnv(t)
	view := e.begin(t, randomUntimed(2), nil)
	if _, err := e.submit.Execute(ctx, view.ID); err != nil {
		t.Fatal(err)
	}

	_, err := e.submit.Execute(ctx, view.ID)

	wantConflict(t, err, domain.RuleAttemptClosed)
}

func TestSubmitAfterTheExpiryIsAttemptClosedAndTheAttemptIsAlreadyScored(t *testing.T) { // S06b-8
	e := newSessionEnv(t)
	view := e.begin(t, timedRandom(4, 10, domain.FeedbackEnd), nil)
	e.save(t, view.ID, 0, false, e.keyOf(view.ID, 0)...)
	e.at(10*time.Minute + 6*time.Second)

	_, err := e.submit.Execute(ctx, view.ID)

	wantConflict(t, err, domain.RuleAttemptClosed)
	stored := e.attempts.Rows[view.ID]
	if stored.Status != domain.AttemptExpired || stored.ScorePercent == nil || *stored.ScorePercent != 25 {
		t.Errorf("stored = %q, score %v; want expired and scored by the submit that found it", stored.Status, stored.ScorePercent)
	}
}

func TestSubmitInsideTheGraceIsAcceptedAndNeverLastsLongerThanTheTimeLimit(t *testing.T) { // BR-A2 grace; phase-3 fix 3
	for name, late := range map[string]time.Duration{"3 s": 3 * time.Second, "4 s": 4 * time.Second, "exactly the grace limit": 5 * time.Second} {
		e := newSessionEnv(t)
		view := e.begin(t, timedRandom(2, 10, domain.FeedbackEnd), nil)
		e.at(10*time.Minute + late)

		r, err := e.submit.Execute(ctx, view.ID)

		stored := e.attempts.Rows[view.ID]
		if err != nil || stored.Status != domain.AttemptSubmitted || r.DurationSeconds != 600 {
			t.Errorf("%s late: result %+v, err %v, status %q; want a submit lasting the time limit, 600 s", name, r, err, stored.Status)
		}
		if stored.SubmittedAt == nil || !stored.SubmittedAt.Equal(t0.Add(10*time.Minute)) {
			t.Errorf("%s late: submittedAt = %v, want the deadline", name, stored.SubmittedAt)
		}
	}
}

func TestSubmitBeforeTheDeadlineKeepsItsOwnTimeAndAnUntimedOneNeverClamps(t *testing.T) { // phase-3 fix 3: min(now, deadlineAt)
	e := newSessionEnv(t)
	timed := e.begin(t, timedRandom(2, 10, domain.FeedbackEnd), nil)
	e.at(9*time.Minute + 59*time.Second)
	r, err := e.submit.Execute(ctx, timed.ID)
	if err != nil || r.DurationSeconds != 599 {
		t.Errorf("timed, 1 s early: %+v, err %v; want 599 s", r, err)
	}

	e2 := newSessionEnv(t)
	untimed := e2.begin(t, randomUntimed(2), nil)
	e2.at(26 * time.Hour)
	r, err = e2.submit.Execute(ctx, untimed.ID)
	if err != nil || r.DurationSeconds != 26*3600 {
		t.Errorf("untimed: %+v, err %v; want 93600 s", r, err)
	}
}

func TestSubmitOfAnUnknownAttemptIsNotFound(t *testing.T) {
	e := newSessionEnv(t)
	if _, err := e.submit.Execute(ctx, "0abcdef0-0000-7000-8000-ffffffffffff"); !isNotFound(err) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
