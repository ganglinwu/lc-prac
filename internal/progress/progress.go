// Package progress persists how each drill has gone and schedules when it
// should come back, so a session resurfaces what you got wrong.
package progress

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	path    string
	records map[string]Record
}

type file struct {
	Version int      `json:"version"`
	Records []Record `json:"records"`
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
	f := file{Version: 1, Records: s.Records()}
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

// Len reports how many drills have any history.
func (s *Store) Len() int { return len(s.records) }

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

// Priority scores a drill for selection; higher is picked sooner. Overdue
// drills beat never-seen ones, which beat drills that are still resting.
func (s *Store) Priority(id string, now time.Time) int {
	r, ok := s.records[id]
	if !ok {
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
	if !ok {
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
