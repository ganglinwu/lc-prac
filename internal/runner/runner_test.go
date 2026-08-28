package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/codecheck"
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
	return runRetry(t, false, input, ds...)
}

func runRetry(t *testing.T, retry bool, input string, ds ...drill.Drill) (Report, string) {
	t.Helper()
	var out strings.Builder
	r := New(strings.NewReader(input), &out)
	r.RetryMisses = retry
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

func TestRetryPassReAsksOnlyMisses(t *testing.T) {
	// wrong choice, right complexity, then the retry answer for the choice.
	rep, out := runRetry(t, true, "1\nO(n log k)\n2\n", choiceDrill(), complexityDrill())
	if correct, attempted := rep.Score(); correct != 1 || attempted != 2 {
		t.Fatalf("Score() = %d/%d, want 1/2", correct, attempted)
	}
	if len(rep.Retries) != 1 {
		t.Fatalf("expected 1 retry, got %d", len(rep.Retries))
	}
	if rep.Retries[0].Drill.ID != "c" || !rep.Retries[0].Correct {
		t.Errorf("retry should be the missed choice drill, answered right: %+v", rep.Retries[0])
	}
	if !strings.Contains(out, "[retry 1/1]") || !strings.Contains(out, "second pass: 1/1") {
		t.Errorf("retry pass not visible in output:\n%s", out)
	}
}

func TestRetryPassDoesNotChangeScore(t *testing.T) {
	// Getting it right on the second pass must not erase the first-pass miss,
	// or the scheduler would stop bringing the drill back.
	rep, _ := runRetry(t, true, "1\n2\n", choiceDrill())
	if correct, attempted := rep.Score(); correct != 0 || attempted != 1 {
		t.Fatalf("Score() = %d/%d, want 0/1", correct, attempted)
	}
	if len(rep.Missed()) != 1 {
		t.Fatalf("Missed() = %d, want 1", len(rep.Missed()))
	}
}

func TestNoRetryPassWhenDisabled(t *testing.T) {
	rep, out := runRetry(t, false, "1\n", choiceDrill())
	if len(rep.Retries) != 0 {
		t.Fatalf("expected no retries, got %d", len(rep.Retries))
	}
	if strings.Contains(out, "second pass") {
		t.Error("disabled retry pass should print nothing")
	}
}

func TestRetryPassStopsOnEOF(t *testing.T) {
	// Input runs out during the retry; the summary should still print.
	rep, out := runRetry(t, true, "1\n", choiceDrill())
	if len(rep.Retries) != 0 {
		t.Fatalf("expected no completed retries, got %d", len(rep.Retries))
	}
	if !strings.Contains(out, "0/1 correct") {
		t.Errorf("summary missing:\n%s", out)
	}
}

func TestSkippedDrillsAreNotRetried(t *testing.T) {
	rep, out := runRetry(t, true, "s\n", choiceDrill())
	if len(rep.Retries) != 0 || len(rep.Missed()) != 0 {
		t.Fatalf("a skip is not a miss: retries=%d missed=%d", len(rep.Retries), len(rep.Missed()))
	}
	if strings.Contains(out, "second pass") {
		t.Error("skips should not trigger a second pass")
	}
}

func codeDrill() drill.Drill {
	return drill.Drill{
		ID: "k", Title: "Add", Kind: drill.KindCode, Topic: "hashmap",
		Difficulty: drill.Easy, EstMinutes: 5, Prompt: "write add",
		Answer: "func add(a, b int) int { return a + b }", Explanation: "sum",
		Code: &drill.CodeSpec{
			Stub:     "func add(a, b int) int { return 0 }",
			Preamble: "type unused struct{}",
			Tests:    "import \"testing\"\n\nfunc TestAdd(t *testing.T) {}",
		},
	}
}

// runCode drives a code drill with a stubbed grader so the tests never shell
// out to the toolchain; codecheck's own tests cover the real compile.
func runCode(t *testing.T, input string, grade func(context.Context, codecheck.Program) (codecheck.Result, error), editor func(string) (string, error), ds ...drill.Drill) (Report, string) {
	t.Helper()
	var out strings.Builder
	r := New(strings.NewReader(input), &out)
	r.RetryMisses = false
	r.Grade = grade
	r.Editor = editor
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

func TestCodeDrillGradesTypedSourceUpToTheMarker(t *testing.T) {
	var got codecheck.Program
	pass := func(_ context.Context, p codecheck.Program) (codecheck.Result, error) {
		got = p
		return codecheck.Result{Passed: true}, nil
	}
	input := "func add(a, b int) int {\n\treturn a + b\n}\n.\n"
	rep, out := runCode(t, input, pass, nil, codeDrill())

	if correct, attempted := rep.Score(); correct != 1 || attempted != 1 {
		t.Fatalf("Score() = %d/%d, want 1/1", correct, attempted)
	}
	if got.Source != "func add(a, b int) int {\n\treturn a + b\n}" {
		t.Fatalf("graded source = %q", got.Source)
	}
	if got.Preamble != "type unused struct{}" || got.Tests == "" {
		t.Fatalf("drill's preamble and tests should reach the grader: %+v", got)
	}
	if !strings.Contains(out, "all tests pass") {
		t.Errorf("a pass should be announced, got:\n%s", out)
	}
}

func TestCodeDrillFailureShowsOutputAndModelAnswer(t *testing.T) {
	fail := func(context.Context, codecheck.Program) (codecheck.Result, error) {
		return codecheck.Result{Output: "add_test.go:9: add(2,3) = 0"}, nil
	}
	rep, out := runCode(t, "func add(a, b int) int { return 0 }\n.\n", fail, nil, codeDrill())
	if correct, _ := rep.Score(); correct != 0 {
		t.Fatalf("failing code should score 0, got %d", correct)
	}
	if !strings.Contains(out, "add(2,3) = 0") {
		t.Error("the test failure output should be shown")
	}
	if !strings.Contains(out, "return a + b") {
		t.Error("a failed code drill should reveal the working version")
	}
}

func TestCodeDrillUsesEditorOnE(t *testing.T) {
	var seeded string
	editor := func(initial string) (string, error) {
		seeded = initial
		return "func add(a, b int) int { return a + b }", nil
	}
	var got codecheck.Program
	pass := func(_ context.Context, p codecheck.Program) (codecheck.Result, error) {
		got = p
		return codecheck.Result{Passed: true}, nil
	}
	rep, _ := runCode(t, "e\n", pass, editor, codeDrill())
	if seeded != codeDrill().Code.Stub {
		t.Fatalf("editor should open on the stub, got %q", seeded)
	}
	if !strings.Contains(got.Source, "a + b") {
		t.Fatalf("edited source should be graded, got %q", got.Source)
	}
	if correct, _ := rep.Score(); correct != 1 {
		t.Fatal("editor path should score like the inline path")
	}
}

func TestCodeDrillFallsBackToSelfGradingWithoutToolchain(t *testing.T) {
	rep, out := runCode(t, "\ny\n", nil, nil, codeDrill())
	if correct, attempted := rep.Score(); correct != 1 || attempted != 1 {
		t.Fatalf("Score() = %d/%d, want the self-graded verdict to count", correct, attempted)
	}
	if !strings.Contains(out, "Did you have it?") {
		t.Errorf("without a grader the drill should reveal and ask, got:\n%s", out)
	}
}

func TestCodeDrillSkipDoesNotGrade(t *testing.T) {
	graded := false
	grade := func(context.Context, codecheck.Program) (codecheck.Result, error) {
		graded = true
		return codecheck.Result{}, nil
	}
	rep, _ := runCode(t, "s\n", grade, nil, codeDrill())
	if graded {
		t.Error("skipping should not compile anything")
	}
	if !rep.Results[0].Skipped {
		t.Error("code drill skip should be recorded as a skip")
	}
}

func TestCodeDrillEndsCleanlyWhenInputRunsOut(t *testing.T) {
	pass := func(context.Context, codecheck.Program) (codecheck.Result, error) {
		return codecheck.Result{Passed: true}, nil
	}
	// No end marker: Ctrl-D is a legitimate way to finish typing.
	rep, out := runCode(t, "func add(a, b int) int { return a + b }\n", pass, nil, codeDrill())
	if len(rep.Results) != 1 || !rep.Results[0].Correct {
		t.Fatalf("EOF should still grade what was typed, got %+v", rep.Results)
	}
	if !strings.Contains(out, "correct in") {
		t.Error("summary should still print")
	}
}
