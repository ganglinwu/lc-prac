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

// runClock drives a session with a clock that advances by step on every read,
// so a test can spend the budget deterministically.
func runClock(t *testing.T, budgetMin int, step time.Duration, retry bool, input string, ds ...drill.Drill) (Report, string) {
	t.Helper()
	var out strings.Builder
	r := New(strings.NewReader(input), &out)
	r.RetryMisses = retry
	now := time.Unix(0, 0)
	r.Now = func() time.Time {
		now = now.Add(step)
		return now
	}
	rep, err := r.Run(session.Session{Drills: ds, BudgetMinutes: budgetMin})
	if err != nil {
		t.Fatal(err)
	}
	return rep, out.String()
}

func TestSessionStopsWhenTheBudgetIsSpent(t *testing.T) {
	// Each clock read costs a minute, so a 5m budget cannot reach drill three.
	rep, out := runClock(t, 5, time.Minute, false, "2\n2\n2\n", choiceDrill(), choiceDrill(), choiceDrill())
	if len(rep.Results) != 2 {
		t.Fatalf("asked %d drills, want 2 before the budget ran out", len(rep.Results))
	}
	if len(rep.Unasked) != 1 {
		t.Fatalf("Unasked = %d, want 1", len(rep.Unasked))
	}
	if !strings.Contains(out, "time is up") {
		t.Error("the stop should be explained in the output")
	}
	if !strings.Contains(out, "not reached") {
		t.Error("the summary should list the drill the clock cut")
	}
}

func TestBudgetIsCheckedBetweenDrillsNotDuringOne(t *testing.T) {
	// A drill that overruns still gets graded rather than being cut off.
	rep, _ := runClock(t, 2, time.Minute, false, "2\n2\n", choiceDrill(), choiceDrill())
	if len(rep.Results) != 1 {
		t.Fatalf("asked %d drills, want the first one to finish", len(rep.Results))
	}
	if !rep.Results[0].Correct {
		t.Error("the in-progress drill should still have been graded")
	}
}

func TestNoTimeLimitRunsEveryDrill(t *testing.T) {
	var out strings.Builder
	r := New(strings.NewReader("2\n2\n2\n"), &out)
	r.RetryMisses = false
	r.NoTimeLimit = true
	now := time.Unix(0, 0)
	r.Now = func() time.Time {
		now = now.Add(time.Minute)
		return now
	}
	rep, err := r.Run(session.Session{Drills: []drill.Drill{choiceDrill(), choiceDrill(), choiceDrill()}, BudgetMinutes: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Results) != 3 || len(rep.Unasked) != 0 {
		t.Fatalf("got %d results and %d unasked, want 3 and 0", len(rep.Results), len(rep.Unasked))
	}
}

func TestSecondPassGetsAQuarterOfTheBudgetPastTheDeadline(t *testing.T) {
	// 8m budget plus a 2m grace: one retry fits, the second does not.
	rep, out := runClock(t, 8, time.Minute, true, "1\n1\n1\n1\n", choiceDrill(), choiceDrill())
	if len(rep.Retries) != 1 {
		t.Fatalf("Retries = %d, want 1 before the grace ran out", len(rep.Retries))
	}
	if !strings.Contains(out, "out of time for the rest of the second pass") {
		t.Error("the truncated second pass should say so")
	}
}

func TestSlowDrillGetsAPaceNote(t *testing.T) {
	// choiceDrill estimates 2m, so a drill that takes 5m is well over 2x.
	_, out := runClock(t, 60, 5*time.Minute, false, "2\n", choiceDrill())
	if !strings.Contains(out, "pace:") {
		t.Errorf("a drill well over its estimate should get a pace note, got:\n%s", out)
	}
}

func hintedDrill() drill.Drill {
	d := choiceDrill()
	d.Hints = []string{"first nudge", "second nudge"}
	return d
}

func TestHintsRevealOneAtATimeAndCount(t *testing.T) {
	rep, out := runWith(t, "h\nh\n2\n", hintedDrill())
	if len(rep.Results) != 1 || rep.Results[0].Hints != 2 {
		t.Fatalf("Hints = %v, want 2", rep.Results)
	}
	if !rep.Results[0].Correct {
		t.Error("a hinted answer is still correct")
	}
	if !strings.Contains(out, "hint 1/2: first nudge") || !strings.Contains(out, "hint 2/2: second nudge") {
		t.Errorf("both hints should appear in order:\n%s", out)
	}
	if strings.Index(out, "first nudge") > strings.Index(out, "second nudge") {
		t.Error("hints came out of order")
	}
}

func TestHintsStopAtTheLastOne(t *testing.T) {
	_, out := runWith(t, "h\nh\nh\n2\n", hintedDrill())
	if !strings.Contains(out, "that was the last hint") {
		t.Errorf("asking past the end should say so:\n%s", out)
	}
}

func TestHintKeyIsAnAnswerWhenDrillHasNoHints(t *testing.T) {
	// "h" must stay usable as an answer, not silently eat the turn.
	rep, out := runWith(t, "h\n", complexityDrill())
	if len(rep.Results) != 1 || rep.Results[0].Correct {
		t.Fatalf("want one wrong result, got %+v", rep.Results)
	}
	if strings.Contains(out, "hint") {
		t.Errorf("no hints to offer, so none should be mentioned:\n%s", out)
	}
}

func TestUnhintedDrillsReportZeroHints(t *testing.T) {
	rep, _ := runWith(t, "2\n", hintedDrill())
	if rep.Results[0].Hints != 0 {
		t.Fatalf("Hints = %d, want 0", rep.Results[0].Hints)
	}
}

func TestSummaryMarksHintedSolvesApart(t *testing.T) {
	_, out := runWith(t, "h\n2\n", hintedDrill())
	if !strings.Contains(out, "~ Choice") {
		t.Errorf("hinted solve should get its own mark:\n%s", out)
	}
}

// runCodeTries is runCode with a try ceiling, for the retry-a-failed-compile path.
func runCodeTries(t *testing.T, tries int, input string, grade func(context.Context, codecheck.Program) (codecheck.Result, error), editor func(string) (string, error), ds ...drill.Drill) (Report, string) {
	t.Helper()
	var out strings.Builder
	r := New(strings.NewReader(input), &out)
	r.RetryMisses = false
	r.Grade = grade
	r.Editor = editor
	r.CodeAttempts = tries
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

// failThenPass fails every compile until the nth, so a test can drive the
// fix-it loop deterministically.
func failThenPass(passOn int) (func(context.Context, codecheck.Program) (codecheck.Result, error), *int) {
	n := 0
	return func(context.Context, codecheck.Program) (codecheck.Result, error) {
		n++
		if n >= passOn {
			return codecheck.Result{Passed: true}, nil
		}
		return codecheck.Result{Output: "add_test.go:9: add(2,3) = 0"}, nil
	}, &n
}

func TestCodeDrillRetryLetsYouFixAndPass(t *testing.T) {
	grade, calls := failThenPass(2)
	input := "func add(a, b int) int { return 0 }\n.\nr\nfunc add(a, b int) int { return a + b }\n.\n"
	rep, out := runCodeTries(t, 3, input, grade, nil, codeDrill())

	if *calls != 2 {
		t.Fatalf("expected two compiles, got %d", *calls)
	}
	if correct, _ := rep.Score(); correct != 1 {
		t.Fatalf("a fix on the second try should score, got %d correct", correct)
	}
	if rep.Results[0].Attempts != 2 {
		t.Errorf("Attempts = %d, want 2", rep.Results[0].Attempts)
	}
	if strings.Contains(out, "working version") {
		t.Error("the answer should not be revealed on a drill you ended up solving")
	}
	if !strings.Contains(out, "2 tries") {
		t.Errorf("summary should show the try count, got:\n%s", out)
	}
}

func TestCodeDrillDecliningRetryRevealsAnswerOnce(t *testing.T) {
	grade, calls := failThenPass(99)
	rep, out := runCodeTries(t, 3, "func add(a, b int) int { return 0 }\n.\nn\n", grade, nil, codeDrill())

	if *calls != 1 {
		t.Fatalf("declining should not compile again, got %d compiles", *calls)
	}
	if correct, attempted := rep.Score(); correct != 0 || attempted != 1 {
		t.Fatalf("Score() = %d/%d, want 0/1", correct, attempted)
	}
	if n := strings.Count(out, "working version"); n != 1 {
		t.Errorf("the answer should be revealed exactly once, got %d:\n%s", n, out)
	}
}

func TestCodeDrillStopsAtTheTryCeiling(t *testing.T) {
	grade, calls := failThenPass(99)
	input := "a\n.\nr\nb\n.\nr\nc\n.\nr\nd\n.\n"
	rep, out := runCodeTries(t, 2, input, grade, nil, codeDrill())

	if *calls != 2 {
		t.Fatalf("the ceiling should cap compiles at 2, got %d", *calls)
	}
	if rep.Results[0].Attempts != 2 || rep.Results[0].Correct {
		t.Fatalf("unexpected result %+v", rep.Results[0])
	}
	if !strings.Contains(out, "try 2 of 2") || !strings.Contains(out, "working version") {
		t.Errorf("running out of tries should say so and reveal, got:\n%s", out)
	}
}

func TestCodeDrillRetryEditorOpensOnYourLastVersion(t *testing.T) {
	grade, _ := failThenPass(2)
	var seeded []string
	editor := func(initial string) (string, error) {
		seeded = append(seeded, initial)
		return "func add(a, b int) int { return a + b }", nil
	}
	rep, _ := runCodeTries(t, 3, "func add(a, b int) int { return 0 }\n.\nr\ne\n", grade, editor, codeDrill())

	if len(seeded) != 1 || seeded[0] != "func add(a, b int) int { return 0 }" {
		t.Fatalf("retry editor should open on the previous attempt, got %q", seeded)
	}
	if correct, _ := rep.Score(); correct != 1 {
		t.Fatal("the edited retry should score")
	}
}

func TestCodeDrillHintsWorkDuringRetry(t *testing.T) {
	d := codeDrill()
	d.Hints = []string{"use a map", "one pass is enough"}
	grade, _ := failThenPass(2)
	input := "func add(a, b int) int { return 0 }\n.\nh\nr\nfunc add(a, b int) int { return a + b }\n.\n"
	rep, out := runCodeTries(t, 3, input, grade, nil, d)

	if !strings.Contains(out, "hint 1/2: use a map") {
		t.Errorf("h at the retry prompt should reveal a hint, got:\n%s", out)
	}
	if rep.Results[0].Hints != 1 {
		t.Errorf("Hints = %d, want 1", rep.Results[0].Hints)
	}
	if !strings.Contains(out, "~ Add") {
		t.Errorf("a hinted solve should be marked ~, got:\n%s", out)
	}
}

func TestCodeDrillStopsOfferingTriesWhenTimeIsUp(t *testing.T) {
	grade, calls := failThenPass(99)
	var out strings.Builder
	r := New(strings.NewReader("func add(a, b int) int { return 0 }\n.\nr\n"), &out)
	r.RetryMisses = false
	r.Grade = grade
	r.CodeAttempts = 3
	now := time.Unix(0, 0)
	r.Now = func() time.Time {
		now = now.Add(time.Minute)
		return now
	}
	// Start at 1m and the deadline lands at 3m, so the drill starts in time
	// but the clock is spent by the time its first compile fails.
	rep, err := r.Run(session.Session{Drills: []drill.Drill{codeDrill()}, BudgetMinutes: 2})
	if err != nil {
		t.Fatal(err)
	}
	if *calls != 1 {
		t.Fatalf("a spent budget should not buy another compile, got %d", *calls)
	}
	if !strings.Contains(out.String(), "out of time for another try") {
		t.Errorf("the drill should say why it stopped:\n%s", out.String())
	}
	if n := strings.Count(out.String(), "working version"); n != 1 {
		t.Errorf("the answer should still be revealed once, got %d", n)
	}
	if len(rep.Results) != 1 || rep.Results[0].Attempts != 1 {
		t.Errorf("Attempts = %+v, want one attempt recorded", rep.Results)
	}
}

func TestCodeDrillKeepsItsTriesWithoutATimeLimit(t *testing.T) {
	grade, calls := failThenPass(99)
	var out strings.Builder
	r := New(strings.NewReader("a\n.\nr\nb\n.\n"), &out)
	r.RetryMisses = false
	r.Grade = grade
	r.CodeAttempts = 2
	r.NoTimeLimit = true
	now := time.Unix(0, 0)
	r.Now = func() time.Time {
		now = now.Add(time.Hour)
		return now
	}
	if _, err := r.Run(session.Session{Drills: []drill.Drill{codeDrill()}, BudgetMinutes: 1}); err != nil {
		t.Fatal(err)
	}
	if *calls != 2 {
		t.Fatalf("-nolimit should keep every try, got %d compiles", *calls)
	}
}
