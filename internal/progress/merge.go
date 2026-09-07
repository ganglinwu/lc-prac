package progress

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"
)

// Encode renders the store in the same shape Save writes, so history can be
// handed to something other than the filesystem without a second format.
func (s *Store) Encode() ([]byte, error) {
	f := file{Version: 1, Records: s.Records(), Sessions: s.sessions, Attempts: s.attempts}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// Decode reads history out of bytes, binding the result to path so a store
// pulled from elsewhere can be saved locally. Empty bytes mean no history.
func Decode(path string, b []byte) (*Store, error) {
	s := New(path)
	if len(b) == 0 {
		return s, nil
	}
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("progress: parsing history: %w", err)
	}
	for _, r := range f.Records {
		if r.DrillID != "" {
			s.records[r.DrillID] = r
		}
	}
	s.sessions = f.Sessions
	for _, a := range f.Attempts {
		if a.Ref != "" {
			s.attempts = append(s.attempts, a)
		}
	}
	return s, nil
}

// Merge folds other into a new store built on s, which is what lets two
// machines practise independently and end up with one history.
//
// There is no common ancestor to diff against, so the rules are chosen to be
// safe when the same sitting shows up on both sides: counters take the larger
// value rather than the sum, and the drill's schedule comes from whichever
// side saw it last. Sessions and attempts are timestamped events, so they are
// unioned and deduped instead.
func (s *Store) Merge(other *Store) *Store {
	out := New(s.path)
	for id, r := range s.records {
		out.records[id] = r
	}
	for id, r := range other.records {
		if mine, ok := out.records[id]; ok {
			out.records[id] = mergeRecord(mine, r)
			continue
		}
		out.records[id] = r
	}
	out.sessions = mergeSessions(s.sessions, other.sessions)
	out.attempts = mergeAttempts(s.attempts, other.attempts)
	return out
}

// mergeRecord reconciles two histories of the same drill. Counters are
// monotonic, so the larger side is the one that has seen more; the streak and
// the schedule are a snapshot, so they come from the later attempt as a set.
func mergeRecord(a, b Record) Record {
	newer, older := a, b
	if newer.LastSeen.Before(older.LastSeen) {
		newer, older = older, newer
	}
	r := newer
	r.Seen = max(a.Seen, b.Seen)
	r.Correct = max(a.Correct, b.Correct)
	r.Assisted = max(a.Assisted, b.Assisted)
	r.Seconds = max(a.Seconds, b.Seconds)
	if r.Note == "" {
		r.Note = older.Note
	}
	r.Solution = mergeSolution(a.Solution, b.Solution)
	return r
}

// mergeSolution keeps the version worth coming back to: a passing attempt
// always beats a failing one, and between equals the later wins.
func mergeSolution(a, b *Solution) *Solution {
	switch {
	case a == nil:
		return b
	case b == nil:
		return a
	case a.Passed != b.Passed:
		if a.Passed {
			return a
		}
		return b
	case b.At.After(a.At):
		return b
	default:
		return a
	}
}

// mergeSessions unions two sitting logs, dropping the duplicates that a
// round trip through another machine creates, and keeping the newest.
func mergeSessions(a, b []Session) []Session {
	seen := map[string]bool{}
	var out []Session
	for _, s := range append(append([]Session{}, a...), b...) {
		k := fmt.Sprintf("%s|%d|%d|%d", s.At.UTC().Format(time.RFC3339Nano), s.Minutes, s.Attempted, s.Correct)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, s)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	if len(out) > maxSessions {
		out = out[len(out)-maxSessions:]
	}
	return out
}

// mergeAttempts unions two problem logs on the same terms as sessions.
func mergeAttempts(a, b []Attempt) []Attempt {
	seen := map[string]bool{}
	var out []Attempt
	for _, at := range append(append([]Attempt{}, a...), b...) {
		k := fmt.Sprintf("%s|%s|%s", at.At.UTC().Format(time.RFC3339Nano), at.Ref, at.Result)
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, at)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	if len(out) > maxAttempts {
		out = out[len(out)-maxAttempts:]
	}
	return out
}
