package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/progress"
)

func TestAttemptResultDefaultsToSolved(t *testing.T) {
	res, rest, err := attemptResult([]string{"56", "took", "20m"})
	if err != nil {
		t.Fatal(err)
	}
	if res != progress.Passed {
		t.Errorf("result = %q, want solved", res)
	}
	if strings.Join(rest, " ") != "56 took 20m" {
		t.Errorf("rest = %v", rest)
	}
}

func TestAttemptResultFlagAfterProblem(t *testing.T) {
	res, rest, err := attemptResult([]string{"merge", "-failed", "got", "the", "sort", "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	if res != progress.Failed {
		t.Errorf("result = %q, want failed", res)
	}
	if strings.Join(rest, " ") != "merge got the sort wrong" {
		t.Errorf("rest = %v", rest)
	}
}

func TestAttemptResultRejectsConflictingAndUnknownFlags(t *testing.T) {
	if _, _, err := attemptResult([]string{"-solved", "-failed", "56"}); err == nil {
		t.Error("want error on two outcome flags")
	}
	if _, _, err := attemptResult([]string{"-nope", "56"}); err == nil {
		t.Error("want error on unknown leading flag")
	}
}

func TestResolveProblemByNumberAndAmbiguousTitle(t *testing.T) {
	set, _ := problemFixture(t)
	ref, err := resolveProblem(set, "739")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(ref, "LC 739") {
		t.Errorf("ref = %q", ref)
	}
	_, err = resolveProblem(set, "s") // in all three fixture titles
	if err == nil || !strings.Contains(err.Error(), "matches 3 problems") {
		t.Errorf("want an ambiguity error, not a guess; got %v", err)
	}
	if _, err := resolveProblem(set, "9999"); err == nil {
		t.Error("want an error for an unknown problem")
	}
}

func TestProblemRowsRankByRealAttempt(t *testing.T) {
	set, store := problemFixture(t)
	now := time.Now()
	store.Record("a", true, now) // LC 20 solid on drills
	store.Record("c", true, now) // LC 70 solid on drills
	store.LogAttempt("LC 20 Valid Parentheses", progress.Failed, "blanked on the stack", now)
	store.LogAttempt("LC 70 Climbing Stairs", progress.Passed, "", now)
	rows := problemRows(set, store, "", now)
	if rows[0].Ref != "LC 20 Valid Parentheses" {
		t.Errorf("first row = %q, want the problem you failed for real", rows[0].Ref)
	}
	var stairs problemRow
	for _, r := range rows {
		if strings.HasPrefix(r.Ref, "LC 70") {
			stairs = r
		}
	}
	if stairs.tier() != 3 || !stairs.Fresh {
		t.Errorf("a freshly solved problem is tier %d fresh=%v, want 3 true", stairs.tier(), stairs.Fresh)
	}
	if !strings.Contains(problemMark(rows[0]), "failed it") {
		t.Errorf("mark = %q, want it to name the real attempt", problemMark(rows[0]))
	}
}

func TestProblemRowsForgetStaleSolve(t *testing.T) {
	set, store := problemFixture(t)
	now := time.Now()
	store.LogAttempt("LC 70 Climbing Stairs", progress.Passed, "", now.Add(-100*24*time.Hour))
	rows := problemRows(set, store, "dp", now)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Fresh || rows[0].tier() != 1 {
		t.Errorf("a solve from 100 days ago is tier %d fresh=%v, want 1 false", rows[0].tier(), rows[0].Fresh)
	}
}

func TestWriteLoggedPointsAtDrillsAfterAMiss(t *testing.T) {
	var buf bytes.Buffer
	writeLogged(&buf, "LC 56 Merge Intervals", progress.Failed, "off-by-one on the merge", true)
	out := buf.String()
	for _, want := range []string{"logged LC 56", "failed", "off-by-one", "drill -problem 56"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	buf.Reset()
	writeLogged(&buf, "LC 56 Merge Intervals", progress.Passed, "", true)
	if strings.Contains(buf.String(), "-problem") {
		t.Errorf("a solve should not nag you back to the drills:\n%s", buf.String())
	}
}

func TestWriteAttemptsListsNewestFirstWithTally(t *testing.T) {
	var buf bytes.Buffer
	writeAttempts(&buf, nil, 0)
	if !strings.Contains(buf.String(), "no real problems logged yet") {
		t.Errorf("empty state = %q", buf.String())
	}
	store := progress.New("")
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	store.LogAttempt("LC 20 Valid Parentheses", progress.Failed, "stack blank", now.Add(-48*time.Hour))
	store.LogAttempt("LC 70 Climbing Stairs", progress.Passed, "", now)
	buf.Reset()
	writeAttempts(&buf, store.Attempts(), 1)
	out := buf.String()
	if !strings.Contains(out, "1 Mar 2026") || !strings.Contains(out, "LC 70") {
		t.Errorf("newest attempt not shown first:\n%s", out)
	}
	if strings.Contains(out, "LC 20") {
		t.Errorf("-n 1 should truncate the list:\n%s", out)
	}
	if !strings.Contains(out, "2 attempt(s): 1 solved, 1 not yet.") {
		t.Errorf("tally should span every attempt:\n%s", out)
	}
}

func TestPullFlagLiftsFromAnywhere(t *testing.T) {
	rest, found := pullFlag([]string{"128", "-new", "hard"}, "-new", "--new")
	if !found || strings.Join(rest, " ") != "128 hard" {
		t.Errorf("rest = %v found = %v", rest, found)
	}
	if _, found := pullFlag([]string{"128"}, "-new", "--new"); found {
		t.Error("want found = false when the flag is absent")
	}
}

func TestNormalizeRefShapesHandTypedProblems(t *testing.T) {
	cases := map[string]string{
		"128 Longest Consecutive Sequence": "LC 128 Longest Consecutive Sequence",
		"lc128 Longest Consecutive":        "LC 128 Longest Consecutive",
		"LC  128   Longest  Consecutive":   "LC 128 Longest Consecutive",
		"Longest Consecutive Sequence":     "Longest Consecutive Sequence",
		"  ":                               "",
	}
	for in, want := range cases {
		if got := normalizeRef(in); got != want {
			t.Errorf("normalizeRef(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNewProblemRefReusesADeckRefAndFlagsCoverage(t *testing.T) {
	set, _ := problemFixture(t)
	ref, covered, err := newProblemRef(set, "lc 20 valid parentheses")
	if err != nil {
		t.Fatal(err)
	}
	if ref != "LC 20 Valid Parentheses" || !covered {
		t.Errorf("ref = %q covered = %v, want the deck's own spelling", ref, covered)
	}
	ref, covered, err = newProblemRef(set, "128 Longest Consecutive Sequence")
	if err != nil {
		t.Fatal(err)
	}
	if ref != "LC 128 Longest Consecutive Sequence" || covered {
		t.Errorf("ref = %q covered = %v, want an uncovered new problem", ref, covered)
	}
	if _, _, err := newProblemRef(set, "   "); err == nil {
		t.Error("want an error when no problem is named")
	}
}

func TestWriteLoggedPointsAtAddWhenUncovered(t *testing.T) {
	var buf bytes.Buffer
	writeLogged(&buf, "LC 128 Longest Consecutive Sequence", progress.Failed, "", false)
	out := buf.String()
	if !strings.Contains(out, "lcprac add") || strings.Contains(out, "-problem") {
		t.Errorf("an uncovered problem should point at add, not a drill session:\n%s", out)
	}
}
