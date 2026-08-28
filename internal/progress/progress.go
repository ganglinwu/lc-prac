// Package progress persists how each drill has gone and schedules when it
// should come back, so a session resurfaces what you got wrong.
package progress

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Outcome is how one attempt went. Assisted sits between the two: the answer
// was right, but only after a hint, so it is not yet worth deferring further.
type Outcome int

const (
	Missed Outcome = iota
	Assisted
	Solved
)

// Record is the running history of one drill.
type Record struct {
	DrillID  string    `json:"drill_id"`
	Seen     int       `json:"seen"`
	Correct  int       `json:"correct"`
	Assisted int       `json:"assisted,omitempty"`
	Streak   int       `json:"streak"`
	LastSeen time.Time `json:"last_seen"`
	DueAt    time.Time `json:"due_at"`
	// Note is your own words on this drill, kept next to the history so the
	// thing that made it click comes back with the drill.
	Note string `json:"note,omitempty"`
	// Seconds is the total time spent on this drill across every attempt,
	// which is what makes pacing (not just accuracy) reviewable.
	Seconds int `json:"seconds,omitempty"`
}

// AvgSeconds is how long an attempt at this drill takes on average, rounded
// to the nearest second. Zero when nothing has been timed.
func (r Record) AvgSeconds() int {
	if r.Seen == 0 || r.Seconds == 0 {
		return 0
	}
	return (r.Seconds + r.Seen/2) / r.Seen
}

// intervals is a Leitner ladder indexed by streak. A miss drops you to 0, so
// the drill comes back inside the same sitting.
var intervals = []time.Duration{
	10 * time.Minute,
	24 * time.Hour,
	3 * 24 * time.Hour,
	7 * 24 * time.Hour,
	21 * 24 * time.Hour,
	60 * 24 * time.Hour,
}

func interval(streak int) time.Duration {
	if streak < 0 {
		streak = 0
	}
	if streak >= len(intervals) {
		streak = len(intervals) - 1
	}
	return intervals[streak]
}

// Store holds every record and knows where to write them back.
type Store struct {
	path     string
	records  map[string]Record
	sessions []Session
}

type file struct {
	Version  int       `json:"version"`
	Records  []Record  `json:"records"`
	Sessions []Session `json:"sessions,omitempty"`
}

// New returns an empty in-memory store bound to path. An empty path makes
// Save a no-op, which is what tests and one-off runs want.
func New(path string) *Store {
	return &Store{path: path, records: map[string]Record{}}
}

// Load reads the store at path, treating a missing file as an empty history.
func Load(path string) (*Store, error) {
	s := New(path)
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("progress: parsing %s: %w", path, err)
	}
	for _, r := range f.Records {
		if r.DrillID != "" {
			s.records[r.DrillID] = r
		}
	}
	s.sessions = f.Sessions
	return s, nil
}

// Save writes the store atomically so an interrupted run cannot truncate it.
func (s *Store) Save() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	f := file{Version: 1, Records: s.Records(), Sessions: s.sessions}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".progress-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(b, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.path)
}

// Path reports where the store writes to.
func (s *Store) Path() string { return s.path }

// Get returns the record for a drill, if it has ever been attempted.
func (s *Store) Get(id string) (Record, bool) {
	r, ok := s.records[id]
	return r, ok
}

// Records returns every record sorted by drill id.
func (s *Store) Records() []Record {
	out := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DrillID < out[j].DrillID })
	return out
}

// Len reports how many drills have been attempted. A drill that only carries
// a note is not practice, so it does not count as history.
func (s *Store) Len() int {
	n := 0
	for _, r := range s.records {
		if r.Seen > 0 {
			n++
		}
	}
	return n
}

// SetNote attaches your own words to a drill, creating a note-only record for
// one you have never attempted. An empty note clears it, dropping the record
// entirely when there is no history under it.
func (s *Store) SetNote(id, note string) {
	note = strings.TrimSpace(note)
	r, ok := s.records[id]
	if !ok {
		if note == "" {
			return
		}
		r = Record{DrillID: id}
	}
	r.Note = note
	if note == "" && r.Seen == 0 {
		delete(s.records, id)
		return
	}
	s.records[id] = r
}

// Note returns your words on a drill, empty if you have not written any.
func (s *Store) Note(id string) string { return s.records[id].Note }

// Noted returns every record carrying a note, ordered by drill id.
func (s *Store) Noted() []Record {
	var out []Record
	for _, r := range s.Records() {
		if r.Note != "" {
			out = append(out, r)
		}
	}
	return out
}

// Record folds one graded attempt into the history and reschedules the drill.
func (s *Store) Record(id string, correct bool, now time.Time) Record {
	o := Missed
	if correct {
		o = Solved
	}
	return s.RecordOutcome(id, o, now)
}

// RecordOutcome is Record with the hinted case spelled out: an assisted answer
// counts towards accuracy but holds the streak where it is, so the drill comes
// back at the same interval instead of climbing the ladder on borrowed help.
func (s *Store) RecordOutcome(id string, o Outcome, now time.Time) Record {
	r := s.records[id]
	r.DrillID = id
	r.Seen++
	switch o {
	case Solved:
		r.Correct++
		r.Streak++
	case Assisted:
		r.Correct++
		r.Assisted++
	default:
		r.Streak = 0
	}
	r.LastSeen = now
	r.DueAt = now.Add(interval(r.Streak))
	s.records[id] = r
	return r
}

// AddTime folds the time one attempt took into the drill's history. It only
// touches a drill that has actually been attempted, so a note-only record
// cannot pick up a duration and read as practice.
func (s *Store) AddTime(id string, d time.Duration) {
	r, ok := s.records[id]
	if !ok || r.Seen == 0 || d <= 0 {
		return
	}
	r.Seconds += int(d.Round(time.Second) / time.Second)
	s.records[id] = r
}

// Timed returns every record that has time recorded against it, ordered by
// drill id.
func (s *Store) Timed() []Record {
	var out []Record
	for _, r := range s.Records() {
		if r.Seconds > 0 && r.Seen > 0 {
			out = append(out, r)
		}
	}
	return out
}

// Priority scores a drill for selection; higher is picked sooner. Overdue
// drills beat never-seen ones, which beat drills that are still resting.
func (s *Store) Priority(id string, now time.Time) int {
	r, ok := s.records[id]
	if !ok || r.Seen == 0 {
		return 100
	}
	if !now.Before(r.DueAt) {
		overdue := int(now.Sub(r.DueAt) / (24 * time.Hour))
		if overdue > 50 {
			overdue = 50
		}
		return 200 + overdue
	}
	p := 50 - 10*r.Streak
	if p < 1 {
		p = 1
	}
	return p
}

// Due reports whether the drill is scheduled to come back by now.
func (s *Store) Due(id string, now time.Time) bool {
	r, ok := s.records[id]
	if !ok || r.Seen == 0 {
		return true
	}
	return !now.Before(r.DueAt)
}

// DefaultPath is where the CLI keeps history: $LCPRAC_HOME, else
// $XDG_DATA_HOME/lcprac, else ~/.local/share/lcprac.
func DefaultPath() (string, error) {
	if h := os.Getenv("LCPRAC_HOME"); h != "" {
		return filepath.Join(h, "progress.json"), nil
	}
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "lcprac", "progress.json"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "lcprac", "progress.json"), nil
}

// Session is one sitting, logged so the habit itself is visible: what the
// per-drill records cannot show is whether you practised at all this week.
type Session struct {
	At        time.Time `json:"at"`
	Minutes   int       `json:"minutes"`
	Attempted int       `json:"attempted"`
	Correct   int       `json:"correct"`
	Assisted  int       `json:"assisted,omitempty"`
	Skipped   int       `json:"skipped,omitempty"`
}

// maxSessions bounds the log so a daily habit cannot grow the file forever.
const maxSessions = 200

// AddSession appends one sitting, dropping the oldest once the log is full.
func (s *Store) AddSession(sess Session) {
	s.sessions = append(s.sessions, sess)
	if len(s.sessions) > maxSessions {
		s.sessions = s.sessions[len(s.sessions)-maxSessions:]
	}
}

// Sessions returns the logged sittings, oldest first.
func (s *Store) Sessions() []Session {
	out := make([]Session, len(s.sessions))
	copy(out, s.sessions)
	return out
}

// RecentSessions returns up to n sittings, newest first.
func (s *Store) RecentSessions(n int) []Session {
	out := s.Sessions()
	sort.Slice(out, func(i, j int) bool { return out[i].At.After(out[j].At) })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// DayStreak counts consecutive days ending today that have a session. A day
// with no practice yet does not break the streak, so it survives until
// tomorrow: the count is taken from yesterday when today is still empty.
func (s *Store) DayStreak(now time.Time) int {
	days := map[string]bool{}
	for _, sess := range s.sessions {
		days[dayKey(sess.At.In(now.Location()))] = true
	}
	d := now
	if !days[dayKey(d)] {
		d = d.AddDate(0, 0, -1)
		if !days[dayKey(d)] {
			return 0
		}
	}
	n := 0
	for days[dayKey(d)] {
		n++
		d = d.AddDate(0, 0, -1)
	}
	return n
}

func dayKey(t time.Time) string { return t.Format("2006-01-02") }

// LeechMisses is how many misses make a drill a leech: three wrong answers is
// past bad luck and into "this one is not sticking".
const LeechMisses = 3

// leechRecovered is the streak at which a leech is considered fixed: two clean
// solves in a row since, so it is no longer worth putting on a review list.
const leechRecovered = 2

// Misses is how many attempts on this drill went wrong.
func (r Record) Misses() int { return r.Seen - r.Correct }

// IsLeech reports whether a drill keeps beating you: missed at least
// LeechMisses times and not yet solved cleanly twice in a row since.
func (r Record) IsLeech() bool {
	return r.Misses() >= LeechMisses && r.Streak < leechRecovered
}

// Leeches returns the drills you keep missing, worst first, then by id so the
// order is stable across runs.
func (s *Store) Leeches() []Record {
	var out []Record
	for _, r := range s.Records() {
		if r.IsLeech() {
			out = append(out, r)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Misses() > out[j].Misses() })
	return out
}
