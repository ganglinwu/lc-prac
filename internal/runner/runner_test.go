package runner

import (
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/session"
)

func choiceDrill() drill.Drill {
	return drill.Drill{
		ID: "c", Title: "Choice", Kind: drill.KindChoice, Topic: "heap",
		Difficulty: drill.Medium, EstMinutes: 2, Prompt: "pick",
		Choices: []string{"wrong", "right"}, Answer: "right", Explanation: "because",
	}
}

func complexityDrill() drill.Drill {
	return drill.Drill{
		ID: "x", Title: "Cost", Kind: drill.KindComplexity, Topic: "complexity",
		Difficulty: drill.Easy, EstMinutes: 2, Prompt: "cost?",
		Answer: "O(n log k)", Explanation: "heap",
	}
}

func recallDrill() drill.Drill {
	return drill.Drill{
		ID: "r", Title: "Recall", Kind: drill.KindRecall, Topic: "dp",
		Difficulty: drill.Easy, EstMinutes: 2, Prompt: "explain",
		Answer: "the answer", Explanation: "why",
	}
}

func TestCheckAnswer(t *testing.T) {
	tests := []struct {
		name  string
		d     drill.Drill
		input string
		want  bool
	}{
		{"choice by number", choiceDrill(), "2", true},
		{"choice wrong number", choiceDrill(), "1", false},
		{"choice out of range", choiceDrill(), "9", false},
		{"choice by text", choiceDrill(), " right ", true},
		{"complexity exact", complexityDrill(), "O(n log k)", true},
		{"complexity loose spacing", complexityDrill(), "o(nlogk)", true},
		{"complexity wrong", complexityDrill(), "O(n log n)", false},
		{"empty is wrong", complexityDrill(), "   ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := checkAnswer(tt.d, tt.input); got != tt.want {
				t.Fatalf("checkAnswer(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func runWith(t *testing.T, input string, ds ...drill.Drill) (Report, string) {
	t.Helper()
	var out strings.Builder
	r := New(strings.NewReader(input), &out)
	tick := 0
	r.Now = func() time.Time {
		tick++
		return time.Unix(int64(tick), 0)
	}
	rep, err := r.Run(session.Session{Drills: ds, BudgetMinutes: 10})
	if err != nil {
		t.Fatal(err)
	}
	return rep, out.String()
}

func TestRunScoresAutoGradedDrills(t *testing.T) {
	rep, out := runWith(t, "2\nO(n log k)\n", choiceDrill(), complexityDrill())
	correct, attempted := rep.Score()
	if correct != 2 || attempted != 2 {
		t.Fatalf("Score() = %d/%d, want 2/2", correct, attempted)
	}
	if !strings.Contains(out, "because") {
		t.Error("explanation should always be shown")
	}
}

func TestRunSelfGradedUsesVerdictNotAnswerText(t *testing.T) {
	// The first line is the reveal keypress; the second is the verdict.
	rep, _ := runWith(t, "\ny\n", recallDrill())
	if correct, attempted := rep.Score(); correct != 1 || attempted != 1 {
		t.Fatalf("Score() = %d/%d, want 1/1", correct, attempted)
	}
	rep, _ = runWith(t, "\nn\n", recallDrill())
	if correct, attempted := rep.Score(); correct != 0 || attempted != 1 {
		t.Fatalf("Score() = %d/%d, want 0/1", correct, attempted)
	}
}

func TestSkippedDrillsAreNotAttempted(t *testing.T) {
	rep, out := runWith(t, "s\n2\n", choiceDrill(), choiceDrill())
	correct, attempted := rep.Score()
	if correct != 1 || attempted != 1 {
		t.Fatalf("Score() = %d/%d, want 1/1 with one skip", correct, attempted)
	}
	if !rep.Results[0].Skipped {
		t.Error("first drill should be marked skipped")
	}
	if !strings.Contains(out, "skipped") {
		t.Error("skip should be acknowledged in the output")
	}
}

func TestRunStopsCleanlyOnEOF(t *testing.T) {
	// Quitting halfway (ctrl-D) should still produce a summary of what was done.
	rep, out := runWith(t, "2\n", choiceDrill(), choiceDrill())
	if len(rep.Results) != 1 {
		t.Fatalf("expected 1 result before EOF, got %d", len(rep.Results))
	}
	if !strings.Contains(out, "1/1 correct") {
		t.Errorf("summary missing from output:\n%s", out)
	}
}
