package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/progress"
)

func TestResolveProblemRefSearchesDeckAndAttempts(t *testing.T) {
	set, store := problemFixture(t)
	store.LogAttempt("LC 261 Graph Valid Tree", progress.Partial, "", time.Now())

	cases := []struct {
		query, want string
	}{
		{"20", "LC 20 Valid Parentheses"},
		{"daily", "LC 739 Daily Temperatures"},
		{"261", "LC 261 Graph Valid Tree"}, // attempt-only, no drill covers it
	}
	for _, c := range cases {
		got, err := resolveProblemRef(set, store, c.query)
		if err != nil {
			t.Fatalf("%q: %v", c.query, err)
		}
		if got != c.want {
			t.Errorf("%q resolved to %q, want %q", c.query, got, c.want)
		}
	}
}

func TestResolveProblemRefRejectsAmbiguousAndUnknown(t *testing.T) {
	set, store := problemFixture(t)
	for _, q := range []string{"", "lc", "9999", "LC"} {
		if _, err := resolveProblemRef(set, store, q); err == nil {
			t.Errorf("query %q resolved, want an error", q)
		}
	}
	// Both stack drills carry a title containing "l" via Valid/Daily.
	if _, err := resolveProblemRef(set, store, "a"); err == nil {
		t.Error("ambiguous query resolved, want an error naming the candidates")
	}
}

func TestProblemDetailShowsDrillsHistoryAndAttempts(t *testing.T) {
	set, store := problemFixture(t)
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	store.Record("a", false, now)
	store.Record("a", true, now)
	store.SetNote("a", "match with a stack")
	store.Record("b", true, now)
	store.SetSolution("b", "func f() {}", true, now)

	var buf bytes.Buffer
	writeProblemDetail(&buf, "LC 20 Valid Parentheses", set, store, now)
	out := buf.String()
	for _, want := range []string{
		"LC 20 Valid Parentheses",
		"66% over 3 attempt(s)",
		"drills behind it (stack)",
		"1/2 right",
		"note: match with a stack",
		"your code kept (passed, 1 Mar 2026)",
		"not logged a real attempt",
		"lcprac drill -problem 20",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("detail missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, " c ") || strings.Contains(out, "LC 70") {
		t.Errorf("detail leaked a drill from another problem:\n%s", out)
	}
}

func TestProblemDetailListsEveryAttemptNewestFirst(t *testing.T) {
	set, store := problemFixture(t)
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	store.LogAttempt("LC 20 Valid Parentheses", progress.Failed, "forgot the empty-stack case", now.AddDate(0, 0, -10))
	store.LogAttempt("LC 20 Valid Parentheses", progress.Passed, "", now)
	store.LogAttempt("LC 70 Climbing Stairs", progress.Passed, "", now)

	var buf bytes.Buffer
	writeProblemDetail(&buf, "LC 20 Valid Parentheses", set, store, now)
	out := buf.String()
	if strings.Contains(out, "Climbing") {
		t.Errorf("detail leaked another problem's attempt:\n%s", out)
	}
	solved, failed := strings.Index(out, "solved"), strings.Index(out, "failed")
	if solved < 0 || failed < 0 || solved > failed {
		t.Errorf("attempts not newest first:\n%s", out)
	}
	if !strings.Contains(out, "forgot the empty-stack case") {
		t.Errorf("attempt note dropped:\n%s", out)
	}
}

func TestProblemDetailForUncoveredProblemPointsAtAdd(t *testing.T) {
	set, store := problemFixture(t)
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	store.LogAttempt("LC 261 Graph Valid Tree", progress.Partial, "union-find", now)

	var buf bytes.Buffer
	writeProblemDetail(&buf, "LC 261 Graph Valid Tree", set, store, now)
	out := buf.String()
	if !strings.Contains(out, "lcprac add -problem 261") {
		t.Errorf("no pointer at add:\n%s", out)
	}
	if strings.Contains(out, "drills behind it") || strings.Contains(out, "warm up") {
		t.Errorf("uncovered problem claimed drills:\n%s", out)
	}
	if n := strings.Count(out, "add -problem 261"); n != 1 {
		t.Errorf("add line printed %d times, want 1:\n%s", n, out)
	}
}
