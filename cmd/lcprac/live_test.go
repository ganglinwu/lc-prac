package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
	"github.com/ganglinwu/lc-prac/internal/runner"
)

func testLive(t *testing.T) (*liveProgress, *bytes.Buffer, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "progress.json")
	store, err := progress.Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out := &bytes.Buffer{}
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	return newLive(store, out, func() time.Time { return now }), out, path
}

func result(id string, correct bool, hints int, skipped bool) runner.Result {
	return runner.Result{Drill: drill.Drill{ID: id}, Correct: correct, Hints: hints, Skipped: skipped}
}

// The point of recording live: a session killed before it ends still leaves
// the answered drills on disk.
func TestLiveRecordPersistsImmediately(t *testing.T) {
	live, _, path := testLive(t)
	live.record(result("a", true, 0, false))

	reloaded, err := progress.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	rec, ok := reloaded.Get("a")
	if !ok {
		t.Fatal("drill a missing from the reloaded history")
	}
	if rec.Seen != 1 || rec.Correct != 1 {
		t.Errorf("record = %+v, want one seen and one correct", rec)
	}
	if len(reloaded.Sessions()) != 0 {
		t.Errorf("sessions = %d, want none until the sitting ends", len(reloaded.Sessions()))
	}
}

func TestLiveCountsOutcomes(t *testing.T) {
	live, _, _ := testLive(t)
	live.record(result("a", true, 0, false))
	live.record(result("b", true, 1, false))
	live.record(result("c", false, 0, false))
	live.record(result("d", false, 0, true))

	if live.recorded != 3 || live.correct != 2 || live.hinted != 1 || live.missed != 1 || live.skipped != 1 {
		t.Errorf("counts = recorded %d correct %d hinted %d missed %d skipped %d",
			live.recorded, live.correct, live.hinted, live.missed, live.skipped)
	}
}

func TestInterruptLogsPartialSession(t *testing.T) {
	live, out, path := testLive(t)
	live.record(result("a", true, 0, false))
	live.record(result("b", false, 0, false))
	live.interrupt()

	reloaded, err := progress.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	sessions := reloaded.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	if sessions[0].Attempted != 2 || sessions[0].Correct != 1 {
		t.Errorf("session = %+v, want 2 attempted and 1 correct", sessions[0])
	}
	if !strings.Contains(out.String(), "stopped early") {
		t.Errorf("output does not report the early stop:\n%s", out)
	}
}

func TestInterruptBeforeAnyDrillRecordsNothing(t *testing.T) {
	live, out, path := testLive(t)
	live.interrupt()

	reloaded, err := progress.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Sessions()) != 0 || reloaded.Len() != 0 {
		t.Errorf("history is not empty: %d sessions, %d records", len(reloaded.Sessions()), reloaded.Len())
	}
	if !strings.Contains(out.String(), "nothing recorded") {
		t.Errorf("output does not say nothing was recorded:\n%s", out)
	}
}

// An interrupt racing the normal end of a session must not log the sitting
// twice, which would inflate the streak and the recent-sessions view.
func TestSessionLoggedOnce(t *testing.T) {
	live, _, path := testLive(t)
	live.record(result("a", true, 0, false))
	if err := saveResults(live, runner.Report{Elapsed: 4 * time.Minute}); err != nil {
		t.Fatalf("saveResults: %v", err)
	}
	live.interrupt()

	reloaded, err := progress.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := len(reloaded.Sessions()); got != 1 {
		t.Fatalf("sessions = %d, want 1", got)
	}
	if got := reloaded.Sessions()[0].Minutes; got != 4 {
		t.Errorf("minutes = %d, want 4 from the finished report", got)
	}
}

func TestSaveResultsSkipsEmptySitting(t *testing.T) {
	live, out, path := testLive(t)
	live.record(result("a", false, 0, true))
	if err := saveResults(live, runner.Report{Elapsed: time.Minute}); err != nil {
		t.Fatalf("saveResults: %v", err)
	}
	reloaded, err := progress.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(reloaded.Sessions()) != 0 {
		t.Errorf("a sitting where everything was skipped was logged")
	}
	if out.Len() != 0 {
		t.Errorf("unexpected output: %s", out)
	}
}

// Pace is only reviewable if the per-drill time lands on disk with the grade.
func TestLiveRecordSavesElapsed(t *testing.T) {
	live, _, path := testLive(t)
	res := result("a", true, 0, false)
	res.Elapsed = 95 * time.Second
	live.record(res)
	skipped := result("b", false, 0, true)
	skipped.Elapsed = time.Minute
	live.record(skipped)

	reloaded, err := progress.Load(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if rec, _ := reloaded.Get("a"); rec.Seconds != 95 {
		t.Errorf("Seconds = %d, want 95", rec.Seconds)
	}
	if _, ok := reloaded.Get("b"); ok {
		t.Error("a skipped drill was timed; it was never attempted")
	}
}
