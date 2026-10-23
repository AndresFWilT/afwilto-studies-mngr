package domain

import (
	"errors"
	"slices"
	"testing"
	"time"
)

var startedAt = time.Date(2026, 10, 23, 9, 0, 0, 0, time.UTC)

func TestStartAttemptFreezesTheSnapshotOfEachDrawnQuestion(t *testing.T) { // slice 06a §1.3 Snapshot
	bank := Bank{Questions: []Question{
		withOrigin(sessQ("a", dom("Seguridad"), svc("VPC")), "Set", 1),
	}}
	cfg := practice(Selection{Kind: SelectOriginSet, Set: "Set"})

	a, err := StartAttempt("att-1", sessExam(), bank, cfg, 5, startedAt)
	if err != nil {
		t.Fatal(err)
	}
	// A later change to the bank must not reach the attempt.
	bank.Questions[0].Stem = "edited"
	bank.Questions[0].Options[0].Text = "edited"
	bank.Questions[0].Categories[0].Category = "edited"

	item := a.Items[0]
	s := item.Snapshot
	if item.Position != 0 || item.QuestionID != "q-a" || s.Key != "a" || s.Type != QuestionSingle || s.Stem != "stem a" || s.Explanation != "explanation a" {
		t.Errorf("item = %+v", item)
	}
	if !slices.Equal(s.Categories, []CategoryRef{dom("Seguridad"), svc("VPC")}) {
		t.Errorf("categories = %v", s.Categories)
	}
	if s.Origin == nil || *s.Origin != (Origin{Set: "Set", Number: 1, NoteRef: "note of a"}) {
		t.Errorf("origin = %+v", s.Origin)
	}
	if s.PaceMinutes != 2 {
		t.Errorf("paceMinutes = %v, want the exam pace 2", s.PaceMinutes)
	}
	if len(s.Options) != 4 {
		t.Fatalf("options = %+v", s.Options)
	}
	labels := ""
	for i, o := range s.Options {
		labels += o.Label
		if o.Text == "edited" {
			t.Error("the snapshot shares memory with the bank")
		}
		if o.Rationale != "why a/"+o.Text[len("a/"):] || o.Correct != (o.Text == "a/b") {
			t.Errorf("option %d = %+v: rationale or correct flag did not follow the option", i, o)
		}
	}
	if labels != "ABCD" {
		t.Errorf("labels = %q, want ABCD in displayed order", labels)
	}
}

func TestStartAttemptFillsTheHeader(t *testing.T) { // slice 06a §1.3
	a, err := StartAttempt("att-1", sessExam(), awsBank(), simulation(), 77, startedAt)
	if err != nil {
		t.Fatal(err)
	}

	if a.ID != "att-1" || a.ExamID != "exam-1" || a.Status != AttemptInProgress || a.Seed != 77 || !a.StartedAt.Equal(startedAt) {
		t.Errorf("attempt = %+v", a)
	}
	if a.SuggestedMinutes != 130 || a.PassPercent != 72 || len(a.Items) != 65 {
		t.Errorf("suggested %d, pass %d, items %d; want 130, 72, 65", a.SuggestedMinutes, a.PassPercent, len(a.Items))
	}
	for i, it := range a.Items {
		if it.Position != i || it.Selected != nil && len(it.Selected) != 0 || it.Flagged || it.Revealed || it.AnsweredAt != nil || it.Correct != nil {
			t.Fatalf("item %d = %+v, want a blank item at position %d", i, it, i)
		}
	}
}

func TestSimulationAttemptIsTimedWithTheExamTime(t *testing.T) { // FR-7, FR-9 deadline
	a, _ := StartAttempt("att-1", sessExam(), awsBank(), simulation(), 1, startedAt)

	if a.DeadlineAt == nil || !a.DeadlineAt.Equal(startedAt.Add(130*time.Minute)) {
		t.Errorf("deadline = %v, want startedAt + 130 min", a.DeadlineAt)
	}
}

func TestTimedAttemptDeadlineIsStartedAtPlusTheChosenMinutes(t *testing.T) { // FR-9
	cfg := SessionConfig{Selection: Selection{Kind: SelectRandom, Count: 5}, Timing: &Timing{Kind: Timed, Minutes: 25}, Feedback: FeedbackEach}
	a, _ := StartAttempt("att-1", sessExam(), awsBank(), cfg, 1, startedAt)

	if a.DeadlineAt == nil || !a.DeadlineAt.Equal(startedAt.Add(25*time.Minute)) {
		t.Errorf("deadline = %v, want startedAt + 25 min", a.DeadlineAt)
	}
}

func TestUntimedAttemptHasNoDeadline(t *testing.T) {
	a, _ := StartAttempt("att-1", sessExam(), awsBank(), practice(Selection{Kind: SelectRandom, Count: 5}), 1, startedAt)
	if a.DeadlineAt != nil || a.RemainingSeconds(startedAt) != nil {
		t.Errorf("deadline %v, remaining %v; want neither", a.DeadlineAt, a.RemainingSeconds(startedAt))
	}
}

func TestRemainingSecondsCountsDownAndStopsAtZero(t *testing.T) { // FR-10 remainingSeconds = max(0, ceil(deadline - now))
	cfg := SessionConfig{Selection: Selection{Kind: SelectRandom, Count: 5}, Timing: &Timing{Kind: Timed, Minutes: 1}, Feedback: FeedbackEnd}
	a, _ := StartAttempt("att-1", sessExam(), awsBank(), cfg, 1, startedAt)

	for name, tc := range map[string]struct {
		now  time.Time
		want int
	}{
		"at the start":         {startedAt, 60},
		"ten seconds in":       {startedAt.Add(10 * time.Second), 50},
		"a fraction rounds up": {startedAt.Add(10*time.Second + 200*time.Millisecond), 50},
		"at the deadline":      {startedAt.Add(time.Minute), 0},
		"after the deadline":   {startedAt.Add(2 * time.Minute), 0},
	} {
		if got := a.RemainingSeconds(tc.now); got == nil || *got != tc.want {
			t.Errorf("%s: remaining = %v, want %d", name, got, tc.want)
		}
	}
}

func TestStartAttemptRejectsAnInvalidConfigBeforeDrawing(t *testing.T) { // S06a-5
	cfg := SessionConfig{Selection: Selection{Kind: SelectSimulation}, Timing: &Timing{Kind: Timed, Minutes: 30}}

	_, err := StartAttempt("att-1", sessExam(), awsBank(), cfg, 1, startedAt)

	var v *ValidationError
	if !errors.As(err, &v) || keysOf(v.Violations)[0] != "config.timing:locked_by_simulation" {
		t.Errorf("err = %v, want locked_by_simulation on config.timing", err)
	}
}

func TestStartAttemptPassesTheInsufficientQuestionsError(t *testing.T) { // AC-15
	_, err := StartAttempt("att-1", sessExam(), Bank{}, simulation(), 1, startedAt)
	if len(shortagesOf(err)) != 1 {
		t.Errorf("err = %v, want insufficient_questions", err)
	}
}

func TestAMultiQuestionCarriesChooseCountEqualToItsCorrectOptions(t *testing.T) { // S06a-7
	multi := sessQ("m")
	multi.Type = QuestionMulti
	multi.Options = []Option{{Text: "m/a", Correct: true}, {Text: "m/b"}, {Text: "m/c", Correct: true}, {Text: "m/d"}}
	single := sessQ("s")
	bank := Bank{Questions: []Question{multi, single}}

	a, err := StartAttempt("att-1", sessExam(), bank, practice(Selection{Kind: SelectRandom, Count: 2}), 3, startedAt)
	if err != nil {
		t.Fatal(err)
	}

	for _, it := range a.Items {
		switch it.Snapshot.Key {
		case "m":
			if got := it.Snapshot.ChooseCount(); got != 2 {
				t.Errorf("multi ChooseCount = %d, want 2", got)
			}
		case "s":
			if got := it.Snapshot.ChooseCount(); got != 0 {
				t.Errorf("single ChooseCount = %d, want 0", got)
			}
		}
	}
}
