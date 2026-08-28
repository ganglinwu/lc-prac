package progress

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

var epoch = time.Date(2026, 8, 28, 9, 0, 0, 0, time.UTC)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.Len() != 0 {
		t.Fatalf("want empty store, got %d records", s.Len())
	}
}

func TestRecordAdvancesStreakAndSchedule(t *testing.T) {
	s := New("")
	r := s.Record("a", true, epoch)
	if r.Seen != 1 || r.Correct != 1 || r.Streak != 1 {
		t.Fatalf("after one hit: %+v", r)
	}
	if want := epoch.Add(24 * time.Hour); !r.DueAt.Equal(want) {
		t.Fatalf("due at %v, want %v", r.DueAt, want)
	}

	r = s.Record("a", false, epoch.Add(25*time.Hour))
	if r.Streak != 0 || r.Seen != 2 || r.Correct != 1 {
		t.Fatalf("after a miss: %+v", r)
	}
	if want := epoch.Add(25*time.Hour + 10*time.Minute); !r.DueAt.Equal(want) {
		t.Fatalf("miss should come back in 10m, got %v want %v", r.DueAt, want)
	}
}

func TestIntervalCapsAtLastRung(t *testing.T) {
	s := New("")
	now := epoch
	for i := 0; i < 8; i++ {
		r := s.Record("a", true, now)
		now = r.DueAt
	}
	r, _ := s.Get("a")
	if got := r.DueAt.Sub(now); got != 0 {
		t.Fatalf("unexpected drift %v", got)
	}
	if r.Streak != 8 {
		t.Fatalf("streak %d, want 8", r.Streak)
	}
}

func TestPriorityOrdersOverdueBeforeUnseenBeforeResting(t *testing.T) {
	s := New("")
	s.Record("missed", false, epoch)
	s.Record("fresh", true, epoch.Add(23*time.Hour))
	now := epoch.Add(24 * time.Hour)

	overdue := s.Priority("missed", now)
	unseen := s.Priority("never", now)
	resting := s.Priority("fresh", now)
	if !(overdue > unseen && unseen > resting) {
		t.Fatalf("want overdue(%d) > unseen(%d) > resting(%d)", overdue, unseen, resting)
	}
	if !s.Due("missed", now) || s.Due("fresh", now) {
		t.Fatalf("Due disagrees with Priority")
	}
	if !s.Due("never", now) {
		t.Fatalf("an unseen drill should always be due")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "progress.json")
	s := New(path)
	s.Record("a", true, epoch)
	s.Record("b", false, epoch)
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Len() != 2 {
		t.Fatalf("loaded %d records, want 2", got.Len())
	}
	a, ok := got.Get("a")
	if !ok || a.Streak != 1 || !a.DueAt.Equal(epoch.Add(24*time.Hour)) {
		t.Fatalf("record a did not survive: %+v ok=%v", a, ok)
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Fatalf("temp file left behind: %v", entries)
	}
}

func TestSaveWithoutPathIsNoOp(t *testing.T) {
	if err := New("").Save(); err != nil {
		t.Fatalf("Save on pathless store: %v", err)
	}
}

func TestDefaultPathPrefersLcpracHome(t *testing.T) {
	t.Setenv("LCPRAC_HOME", "/tmp/lc")
	got, err := DefaultPath()
	if err != nil || got != "/tmp/lc/progress.json" {
		t.Fatalf("DefaultPath = %q, %v", got, err)
	}
	t.Setenv("LCPRAC_HOME", "")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg")
	got, _ = DefaultPath()
	if got != "/tmp/xdg/lcprac/progress.json" {
		t.Fatalf("DefaultPath with XDG = %q", got)
	}
}
