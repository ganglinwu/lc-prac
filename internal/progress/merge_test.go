package progress

import (
	"path/filepath"
	"testing"
	"time"
)

// roundTrip is what a sync does to a store: encode it, hand it over, read it
// back somewhere else.
func roundTrip(t *testing.T, s *Store) *Store {
	t.Helper()
	b, err := s.Encode()
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(filepath.Join(t.TempDir(), "progress.json"), b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return got
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	s := New("")
	s.Record("a", true, epoch)
	s.AddTime("a", 90*time.Second)
	s.SetSolution("a", "func f() {}", true, epoch)
	s.SetNote("b", "two pointers, not three")
	s.AddSession(Session{At: epoch, Minutes: 12, Attempted: 4, Correct: 3})
	s.LogAttempt("56", Passed, "merge intervals", epoch)

	got := roundTrip(t, s)
	if r, ok := got.Get("a"); !ok || r.Seen != 1 || r.Seconds != 90 || r.Solution == nil {
		t.Fatalf("record a did not survive: %+v", r)
	}
	if got.Note("b") != "two pointers, not three" {
		t.Fatalf("note did not survive: %q", got.Note("b"))
	}
	if len(got.Sessions()) != 1 || len(got.Attempts()) != 1 {
		t.Fatalf("logs did not survive: %d sessions, %d attempts", len(got.Sessions()), len(got.Attempts()))
	}
}

func TestDecodeEmptyIsEmpty(t *testing.T) {
	s, err := Decode("", nil)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if s.Len() != 0 {
		t.Fatalf("want empty store, got %d", s.Len())
	}
}

func TestMergeTakesRecordsOnlyOneSideHas(t *testing.T) {
	a, b := New("/tmp/a.json"), New("")
	a.Record("only-a", true, epoch)
	b.Record("only-b", false, epoch)

	got := a.Merge(b)
	if _, ok := got.Get("only-a"); !ok {
		t.Fatal("lost the local drill")
	}
	if _, ok := got.Get("only-b"); !ok {
		t.Fatal("lost the remote drill")
	}
	if got.Path() != "/tmp/a.json" {
		t.Fatalf("merge should stay bound to the local path, got %q", got.Path())
	}
	if _, ok := a.Get("only-b"); ok {
		t.Fatal("merge must not mutate the receiver")
	}
}

// The same session synced twice must not double a drill's counters, which is
// the failure mode that would quietly wreck accuracy.
func TestMergeOfIdenticalHistoriesIsUnchanged(t *testing.T) {
	a := New("")
	a.Record("x", true, epoch)
	a.Record("x", false, epoch.Add(time.Hour))
	a.AddTime("x", 30*time.Second)
	a.AddSession(Session{At: epoch, Minutes: 12, Attempted: 2, Correct: 1})
	a.LogAttempt("56", Failed, "", epoch)

	got := a.Merge(roundTrip(t, a))
	r, _ := got.Get("x")
	if r.Seen != 2 || r.Correct != 1 || r.Seconds != 30 {
		t.Fatalf("counters inflated on re-sync: %+v", r)
	}
	if len(got.Sessions()) != 1 {
		t.Fatalf("session duplicated: %d", len(got.Sessions()))
	}
	if len(got.Attempts()) != 1 {
		t.Fatalf("attempt duplicated: %d", len(got.Attempts()))
	}
}

func TestMergeTakesScheduleFromLaterAttempt(t *testing.T) {
	later := epoch.Add(48 * time.Hour)
	a, b := New(""), New("")
	a.Record("x", true, epoch)  // streak 1, due in a day
	b.Record("x", false, later) // missed later, so it should come back soon

	got := a.Merge(b)
	r, _ := got.Get("x")
	if r.Streak != 0 {
		t.Fatalf("streak should come from the later attempt, got %d", r.Streak)
	}
	if want := later.Add(10 * time.Minute); !r.DueAt.Equal(want) {
		t.Fatalf("due at %v, want %v", r.DueAt, want)
	}
	if r.Seen != 1 || r.Correct != 1 {
		t.Fatalf("counters should take the larger side: %+v", r)
	}

	// Merging the other way round has to land on the same answer.
	if other, _ := b.Merge(a).Get("x"); other != r {
		t.Fatalf("merge is not symmetric:\n a<-b %+v\n b<-a %+v", r, other)
	}
}

func TestMergeKeepsNoteFromTheSideThatHasOne(t *testing.T) {
	a, b := New(""), New("")
	a.Record("x", true, epoch.Add(time.Hour))
	b.Record("x", true, epoch)
	b.SetNote("x", "sort by end time")

	if got := a.Merge(b).Note("x"); got != "sort by end time" {
		t.Fatalf("note lost to the side without one: %q", got)
	}
}

func TestMergePrefersPassingSolution(t *testing.T) {
	a, b := New(""), New("")
	a.Record("x", true, epoch.Add(time.Hour))
	a.SetSolution("x", "broken", false, epoch.Add(time.Hour))
	b.Record("x", true, epoch)
	b.SetSolution("x", "works", true, epoch)

	sol, ok := a.Merge(b).Solution("x")
	if !ok || sol.Source != "works" {
		t.Fatalf("want the passing version, got %+v", sol)
	}
}

func TestMergeUnionsDistinctSessionsAndAttempts(t *testing.T) {
	a, b := New(""), New("")
	a.AddSession(Session{At: epoch, Minutes: 12, Attempted: 4, Correct: 4})
	b.AddSession(Session{At: epoch.Add(24 * time.Hour), Minutes: 8, Attempted: 3, Correct: 1})
	a.LogAttempt("56", Passed, "", epoch)
	b.LogAttempt("57", Failed, "", epoch.Add(24*time.Hour))

	got := a.Merge(b)
	sessions := got.Sessions()
	if len(sessions) != 2 || !sessions[0].At.Equal(epoch) {
		t.Fatalf("want both sittings oldest first, got %+v", sessions)
	}
	if n := got.DayStreak(epoch.Add(24 * time.Hour)); n != 2 {
		t.Fatalf("merged sittings should build a 2 day streak, got %d", n)
	}
	if len(got.Attempts()) != 2 {
		t.Fatalf("want both attempts, got %+v", got.Attempts())
	}
}

func TestMergeCapsTheLogs(t *testing.T) {
	a, b := New(""), New("")
	for i := 0; i < maxSessions; i++ {
		a.AddSession(Session{At: epoch.Add(time.Duration(i) * time.Hour), Minutes: 1, Attempted: 1})
		b.AddSession(Session{At: epoch.Add(time.Duration(i)*time.Hour + 30*time.Minute), Minutes: 1, Attempted: 1})
	}
	got := a.Merge(b)
	if len(got.Sessions()) != maxSessions {
		t.Fatalf("want the log capped at %d, got %d", maxSessions, len(got.Sessions()))
	}
	// The cap must drop the oldest, not the newest.
	newest := got.RecentSessions(1)[0]
	if want := epoch.Add(time.Duration(maxSessions-1)*time.Hour + 30*time.Minute); !newest.At.Equal(want) {
		t.Fatalf("newest sitting is %v, want %v", newest.At, want)
	}
}
