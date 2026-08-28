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

func TestAssistedHoldsTheStreak(t *testing.T) {
	s := New("")
	now := time.Now()
	s.Record("d", true, now)
	s.Record("d", true, now)
	before, _ := s.Get("d")

	r := s.RecordOutcome("d", Assisted, now)
	if r.Streak != before.Streak {
		t.Fatalf("streak = %d, want it held at %d", r.Streak, before.Streak)
	}
	if r.Correct != before.Correct+1 || r.Assisted != 1 {
		t.Fatalf("assisted attempt should count as correct: %+v", r)
	}
	if want := now.Add(interval(before.Streak)); !r.DueAt.Equal(want) {
		t.Fatalf("DueAt = %v, want the same rung %v", r.DueAt, want)
	}
}

func TestMissedOutcomeMatchesRecordFalse(t *testing.T) {
	now := time.Now()
	a, b := New(""), New("")
	a.Record("d", true, now)
	b.Record("d", true, now)
	if a.Record("d", false, now) != b.RecordOutcome("d", Missed, now) {
		t.Fatal("Record(false) and RecordOutcome(Missed) should agree")
	}
}

func TestSessionsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "progress.json")
	s := New(path)
	base := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	s.Record("a", true, base)
	s.AddSession(Session{At: base, Minutes: 12, Attempted: 5, Correct: 4, Assisted: 1, Skipped: 2})
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	sessions := got.Sessions()
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(sessions))
	}
	if sessions[0].Attempted != 5 || sessions[0].Correct != 4 || sessions[0].Minutes != 12 {
		t.Errorf("session round-tripped as %+v", sessions[0])
	}
	if !sessions[0].At.Equal(base) {
		t.Errorf("At = %v, want %v", sessions[0].At, base)
	}
}

func TestSessionLogIsBounded(t *testing.T) {
	s := New("")
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < maxSessions+10; i++ {
		s.AddSession(Session{At: base.Add(time.Duration(i) * time.Hour), Attempted: i})
	}
	got := s.Sessions()
	if len(got) != maxSessions {
		t.Fatalf("len = %d, want %d", len(got), maxSessions)
	}
	if got[0].Attempted != 10 {
		t.Errorf("oldest kept is %d, want the 10th (earliest dropped)", got[0].Attempted)
	}
}

func TestRecentSessionsAreNewestFirst(t *testing.T) {
	s := New("")
	base := time.Date(2026, 3, 1, 8, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		s.AddSession(Session{At: base.AddDate(0, 0, i), Attempted: i})
	}
	got := s.RecentSessions(3)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Attempted != 7 || got[2].Attempted != 5 {
		t.Errorf("recent = %d,%d,%d, want 7,6,5", got[0].Attempted, got[1].Attempted, got[2].Attempted)
	}
}

func TestDayStreak(t *testing.T) {
	now := time.Date(2026, 8, 29, 21, 0, 0, 0, time.UTC)
	day := func(n int) time.Time { return now.AddDate(0, 0, -n) }
	tests := []struct {
		name string
		at   []time.Time
		want int
	}{
		{"empty", nil, 0},
		{"today only", []time.Time{day(0)}, 1},
		{"today and yesterday", []time.Time{day(1), day(0)}, 2},
		{"yesterday still counts", []time.Time{day(2), day(1)}, 2},
		{"two days idle breaks it", []time.Time{day(3), day(2)}, 0},
		{"twice in a day counts once", []time.Time{day(0), day(0).Add(-3 * time.Hour), day(1)}, 2},
		{"gap stops the walk", []time.Time{day(5), day(4), day(1), day(0)}, 2},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New("")
			for _, at := range tc.at {
				s.AddSession(Session{At: at, Attempted: 1})
			}
			if got := s.DayStreak(now); got != tc.want {
				t.Errorf("DayStreak = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestLoadWithoutSessionsIsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "progress.json")
	old := `{"version":1,"records":[{"drill_id":"a","seen":1,"correct":1,"streak":1}]}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Sessions()) != 0 {
		t.Errorf("sessions = %d, want 0 for a pre-session history file", len(s.Sessions()))
	}
	if s.Len() != 1 {
		t.Errorf("records = %d, want 1", s.Len())
	}
}

func TestLeechNeedsThreeMissesAndNoRecovery(t *testing.T) {
	cases := []struct {
		name string
		rec  Record
		want bool
	}{
		{"two misses is bad luck", Record{Seen: 2, Correct: 0}, false},
		{"three misses sticks", Record{Seen: 4, Correct: 1}, true},
		{"one clean solve since is not enough", Record{Seen: 4, Correct: 1, Streak: 1}, true},
		{"two clean solves since clears it", Record{Seen: 5, Correct: 2, Streak: 2}, false},
	}
	for _, c := range cases {
		if got := c.rec.IsLeech(); got != c.want {
			t.Errorf("%s: IsLeech = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestLeechesAreWorstFirst(t *testing.T) {
	now := time.Now()
	s := New("")
	for i := 0; i < 3; i++ {
		s.Record("mild", false, now)
	}
	for i := 0; i < 5; i++ {
		s.Record("awful", false, now)
	}
	s.Record("fine", true, now)
	got := s.Leeches()
	if len(got) != 2 {
		t.Fatalf("got %d leeches, want 2: %+v", len(got), got)
	}
	if got[0].DrillID != "awful" || got[1].DrillID != "mild" {
		t.Errorf("order = %s, %s; want awful, mild", got[0].DrillID, got[1].DrillID)
	}
}

// A note on a drill you have never attempted must survive a save/load round
// trip without counting as practice or as a due drill.
func TestNoteOnUnseenDrill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "progress.json")
	s := New(path)
	s.SetNote("two-pointers-1", "  left never rewinds  ")
	if got := s.Note("two-pointers-1"); got != "left never rewinds" {
		t.Fatalf("Note = %q, want the trimmed text", got)
	}
	if s.Len() != 0 {
		t.Fatalf("Len = %d, a note-only record is not history", s.Len())
	}
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	if p := s.Priority("two-pointers-1", now); p != 100 {
		t.Fatalf("Priority = %d, want the never-seen score 100", p)
	}
	if !s.Due("two-pointers-1", now) {
		t.Fatal("a note-only drill should still be due like an unseen one")
	}
	if err := s.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := back.Note("two-pointers-1"); got != "left never rewinds" {
		t.Fatalf("after reload Note = %q", got)
	}
}

func TestSetNoteKeepsHistoryAndClears(t *testing.T) {
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	s := New("")
	s.Record("a", false, now)
	s.SetNote("a", "watch the empty window")
	r, ok := s.Get("a")
	if !ok || r.Seen != 1 || r.Note != "watch the empty window" {
		t.Fatalf("record = %+v, ok = %v", r, ok)
	}
	if n := s.Noted(); len(n) != 1 || n[0].DrillID != "a" {
		t.Fatalf("Noted = %+v", n)
	}

	s.SetNote("a", "")
	if r, ok := s.Get("a"); !ok || r.Seen != 1 || r.Note != "" {
		t.Fatalf("clearing a note must keep the history: %+v ok=%v", r, ok)
	}
	s.SetNote("b", "throwaway")
	s.SetNote("b", "")
	if _, ok := s.Get("b"); ok {
		t.Fatal("clearing the only reason a record exists should drop it")
	}
	if n := s.Noted(); len(n) != 0 {
		t.Fatalf("Noted = %+v, want none", n)
	}
}
