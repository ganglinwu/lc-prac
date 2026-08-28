package codecheck

import (
	"context"
	"strings"
	"testing"
)

const addTests = `import "testing"

func TestAdd(t *testing.T) {
	if got := add(2, 3); got != 5 {
		t.Fatalf("add(2,3) = %d, want 5", got)
	}
}`

func requireToolchain(t *testing.T) {
	t.Helper()
	if !Available() {
		t.Skip("no go toolchain on PATH")
	}
}

func TestRunPasses(t *testing.T) {
	requireToolchain(t)
	res, err := Run(context.Background(), Program{
		Source: "func add(a, b int) int { return a + b }",
		Tests:  addTests,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Passed {
		t.Fatalf("expected pass, got output:\n%s", res.Output)
	}
}

func TestRunReportsFailingTest(t *testing.T) {
	requireToolchain(t)
	res, err := Run(context.Background(), Program{
		Source: "func add(a, b int) int { return a - b }",
		Tests:  addTests,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Passed {
		t.Fatal("wrong solution reported as passing")
	}
	if !strings.Contains(res.Output, "want 5") {
		t.Fatalf("failure output should quote the test message, got:\n%s", res.Output)
	}
}

func TestRunReportsCompileError(t *testing.T) {
	requireToolchain(t)
	res, err := Run(context.Background(), Program{
		Source: "func add(a, b int) int { return a +",
		Tests:  addTests,
	})
	if err != nil {
		t.Fatalf("a syntax error is a failed drill, not a grading error: %v", err)
	}
	if res.Passed {
		t.Fatal("unparseable source reported as passing")
	}
	if res.Output == "" {
		t.Fatal("compile error should surface the compiler output")
	}
}

func TestRunCompilesPreambleAlongsideSource(t *testing.T) {
	requireToolchain(t)
	res, err := Run(context.Background(), Program{
		Preamble: "type pair struct{ a, b int }",
		Source:   "func add(a, b int) int { p := pair{a, b}; return p.a + p.b }",
		Tests:    addTests,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Passed {
		t.Fatalf("preamble types should be visible to the solution, got:\n%s", res.Output)
	}
}

func TestRunTimesOutOnInfiniteLoop(t *testing.T) {
	requireToolchain(t)
	if testing.Short() {
		t.Skip("timeout test is slow")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cheaper than waiting out DefaultTimeout: a dead context kills it the same way
	res, err := Run(ctx, Program{
		Source: "func add(a, b int) int { for { } }",
		Tests:  addTests,
	})
	if err != nil {
		t.Fatalf("a killed run is a failed drill, not a grading error: %v", err)
	}
	if res.Passed {
		t.Fatal("killed run reported as passing")
	}
}

func TestJoinFileSkipsEmptyParts(t *testing.T) {
	got := joinFile("", "func f() {}", "  ")
	want := "package lcdrill\n\nfunc f() {}\n"
	if got != want {
		t.Fatalf("joinFile = %q, want %q", got, want)
	}
}
