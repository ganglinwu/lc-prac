package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
	"github.com/ganglinwu/lc-prac/internal/runner"
)

// answered builds the report a sitting would produce from drill ids in the
// fixture, marking the wrong ones.
func answered(set *drill.Set, wrong map[string]bool, skipped map[string]bool, ids ...string) runner.Report {
	var rep runner.Report
	for _, id := range ids {
		d, ok := set.ByID(id)
		if !ok {
			continue
		}
		rep.Results = append(rep.Results, runner.Result{
			Drill:   d,
			Correct: !wrong[id],
			Skipped: skipped[id],
		})
	}
	return rep
}

func TestSessionRefsIgnoreSkippedAndTrackMisses(t *testing.T) {
	set, _ := problemFixture(t)
	rep := answered(set, map[string]bool{"b": true}, map[string]bool{"c": true}, "a", "b", "c", "d")
	drilled, missed := sessionRefs(rep)
	if len(drilled) != 2 || !drilled["LC 20 Valid Parentheses"] || !drilled["LC 739 Daily Temperatures"] {
		t.Errorf("drilled = %v, want only the refs of a and b", drilled)
	}
	if len(missed) != 2 {
		t.Errorf("missed = %v, want both refs of the drill answered wrong", missed)
	}
}

func TestSuggestProblemPrefersOneYouMissed(t *testing.T) {
	set, store := problemFixture(t)
	now := time.Now()
	store.Record("a", true, now) // LC 20 solid, so ranked below LC 70
	rep := answered(set, map[string]bool{"c": true}, nil, "a", "c")

	var buf bytes.Buffer
	suggestProblem(&buf, set, store, rep, now)
	out := buf.String()
	if !strings.Contains(out, "now attempt LC 70 Climbing Stairs for real: you missed a drill behind it just now.") {
		t.Errorf("output = %q, want the missed problem named", out)
	}
	if !strings.Contains(out, "`lcprac attempt 70`") {
		t.Errorf("output = %q, want the attempt command by number", out)
	}
}

func TestSuggestProblemFallsBackToRanking(t *testing.T) {
	set, store := problemFixture(t)
	now := time.Now()
	store.Record("a", false, now) // LC 20 shaky, top of the ranking
	rep := answered(set, nil, nil, "a", "c")

	var buf bytes.Buffer
	suggestProblem(&buf, set, store, rep, now)
	if got := buf.String(); !strings.Contains(got, "now attempt LC 20 Valid Parentheses for real: you just drilled its pattern.") {
		t.Errorf("output = %q, want the weakest drilled problem with no miss wording", got)
	}
}

func TestSuggestProblemSkipsFreshSolveAndSaysNothingWithoutRefs(t *testing.T) {
	set, store := problemFixture(t)
	now := time.Now()
	store.LogAttempt("LC 20 Valid Parentheses", progress.Passed, "", now)
	rep := answered(set, nil, nil, "a") // only LC 20, solved for real yesterday

	var buf bytes.Buffer
	suggestProblem(&buf, set, store, rep, now)
	if buf.Len() != 0 {
		t.Errorf("output = %q, want nothing when the only problem is freshly solved", buf.String())
	}

	buf.Reset()
	suggestProblem(&buf, set, store, answered(set, nil, nil, "d"), now)
	if buf.Len() != 0 {
		t.Errorf("output = %q, want nothing when no drill named a problem", buf.String())
	}
}
