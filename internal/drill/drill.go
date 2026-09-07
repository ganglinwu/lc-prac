// Package drill defines the practice units of lc-prac. A drill is a bite-sized
// exercise (2-6 minutes) rather than a whole LeetCode problem.
package drill

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Kind is the shape of a drill, which determines how the CLI presents it and
// how an answer is checked.
type Kind string

const (
	// KindRecall asks a free-form question; the answer is self-graded.
	KindRecall Kind = "recall"
	// KindChoice is multiple choice; the answer is auto-graded.
	KindChoice Kind = "choice"
	// KindComplexity asks for big-O of a described approach, auto-graded.
	KindComplexity Kind = "complexity"
	// KindSnippet asks for a few lines of code; self-graded against a model answer.
	KindSnippet Kind = "snippet"
	// KindCode asks for a working function, graded by compiling and running it.
	KindCode Kind = "code"
)

// Difficulty mirrors LeetCode's scale so drills can be tied back to problems.
type Difficulty string

const (
	Easy   Difficulty = "easy"
	Medium Difficulty = "medium"
	Hard   Difficulty = "hard"
)

// CodeSpec is the machine-checkable half of a code drill: what the user starts
// from, what compiles alongside their code, and the test file that grades it.
type CodeSpec struct {
	// Stub is the starting point the user edits, typically an empty function.
	Stub string `json:"stub"`
	// Preamble is support code (helper types, imports) compiled alongside the
	// user's source but not shown as theirs to write.
	Preamble string `json:"preamble,omitempty"`
	// Tests is a complete Go test file body: everything after the package
	// clause, including its own imports. The drill author owns it entirely so
	// no import juggling is needed at generation time.
	Tests string `json:"tests"`
}

// Drill is a single practice unit.
type Drill struct {
	ID string `json:"id"`
	// Everything below the id is omitempty so a tombstone writes as an id and
	// a timestamp rather than as a drill with every field blanked. A live
	// drill has to fill them all in anyway, so nothing real is dropped.
	Title       string     `json:"title,omitempty"`
	Kind        Kind       `json:"kind,omitempty"`
	Topic       string     `json:"topic,omitempty"`
	Difficulty  Difficulty `json:"difficulty,omitempty"`
	EstMinutes  int        `json:"est_minutes,omitempty"`
	Prompt      string     `json:"prompt,omitempty"`
	Choices     []string   `json:"choices,omitempty"`
	Answer      string     `json:"answer,omitempty"`
	Explanation string     `json:"explanation,omitempty"`
	// Hints are progressive nudges, revealed one at a time on request. Each
	// should narrow the search without naming the answer.
	Hints []string  `json:"hints,omitempty"`
	Refs  []string  `json:"refs,omitempty"`
	Code  *CodeSpec `json:"code,omitempty"`
	// UpdatedAt stamps your own drills so cross-machine sync can tell which
	// copy is the later edit. Builtin drills leave it nil; so do drills you
	// wrote before stamping existed, which makes them lose to any newer copy.
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
	// DeletedAt turns the drill into a tombstone: the id stays so the deletion
	// can travel to your other machines, but nothing practises it. Without
	// this a delete would be undone by the next pull, since a merge that only
	// unions can never learn that something went away.
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
	// Source is where the drill was loaded from, filled in by the loader
	// rather than by the file itself.
	Source string `json:"-"`
}

// SelfGraded reports whether the user grades their own answer. Free-form kinds
// cannot be string-compared, so the CLI shows the model answer and asks.
func (d Drill) SelfGraded() bool {
	return d.Kind == KindRecall || d.Kind == KindSnippet
}

// Deleted reports whether the drill is a tombstone rather than something to
// practise.
func (d Drill) Deleted() bool { return d.DeletedAt != nil }

// Tombstone is the deleted marker for an id, carrying no content: a deletion
// should not keep shipping the text of what was deleted around.
func Tombstone(id string, at time.Time) Drill {
	at = at.UTC()
	return Drill{ID: id, DeletedAt: &at}
}

// Validate reports the first structural problem with a drill, if any. A
// tombstone is checked on its id alone, since the rest of it is gone.
func (d Drill) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return fmt.Errorf("drill: missing id")
	}
	if d.Deleted() {
		return nil
	}
	if strings.TrimSpace(d.Title) == "" {
		return fmt.Errorf("drill %s: missing title", d.ID)
	}
	switch d.Kind {
	case KindRecall, KindChoice, KindComplexity, KindSnippet, KindCode:
	default:
		return fmt.Errorf("drill %s: unknown kind %q", d.ID, d.Kind)
	}
	switch d.Difficulty {
	case Easy, Medium, Hard:
	default:
		return fmt.Errorf("drill %s: unknown difficulty %q", d.ID, d.Difficulty)
	}
	if strings.TrimSpace(d.Topic) == "" {
		return fmt.Errorf("drill %s: missing topic", d.ID)
	}
	if d.EstMinutes < 1 || d.EstMinutes > 10 {
		return fmt.Errorf("drill %s: est_minutes %d out of range 1..10", d.ID, d.EstMinutes)
	}
	if strings.TrimSpace(d.Prompt) == "" {
		return fmt.Errorf("drill %s: missing prompt", d.ID)
	}
	if strings.TrimSpace(d.Answer) == "" {
		return fmt.Errorf("drill %s: missing answer", d.ID)
	}
	if strings.TrimSpace(d.Explanation) == "" {
		return fmt.Errorf("drill %s: missing explanation", d.ID)
	}
	if len(d.Hints) > 3 {
		return fmt.Errorf("drill %s: %d hints, at most 3", d.ID, len(d.Hints))
	}
	for i, h := range d.Hints {
		if strings.TrimSpace(h) == "" {
			return fmt.Errorf("drill %s: hint %d is empty", d.ID, i+1)
		}
	}
	if d.Kind == KindChoice {
		if len(d.Choices) < 2 {
			return fmt.Errorf("drill %s: choice kind needs at least 2 choices", d.ID)
		}
		if !contains(d.Choices, d.Answer) {
			return fmt.Errorf("drill %s: answer %q is not one of the choices", d.ID, d.Answer)
		}
	}
	if d.Kind != KindChoice && len(d.Choices) > 0 {
		return fmt.Errorf("drill %s: choices only allowed on kind %q", d.ID, KindChoice)
	}
	if d.Kind == KindCode {
		if d.Code == nil {
			return fmt.Errorf("drill %s: code kind needs a code block", d.ID)
		}
		if strings.TrimSpace(d.Code.Stub) == "" {
			return fmt.Errorf("drill %s: code block needs a stub", d.ID)
		}
		if strings.TrimSpace(d.Code.Tests) == "" {
			return fmt.Errorf("drill %s: code block needs tests", d.ID)
		}
	} else if d.Code != nil {
		return fmt.Errorf("drill %s: code block only allowed on kind %q", d.ID, KindCode)
	}
	return nil
}

// Set is a validated, immutable collection of drills.
type Set struct {
	drills []Drill
	byID   map[string]int
}

// NewSet validates every drill and rejects duplicate IDs.
func NewSet(drills []Drill) (*Set, error) {
	byID := make(map[string]int, len(drills))
	for i, d := range drills {
		if err := d.Validate(); err != nil {
			return nil, err
		}
		if _, dup := byID[d.ID]; dup {
			return nil, fmt.Errorf("duplicate drill id %q", d.ID)
		}
		byID[d.ID] = i
	}
	cp := make([]Drill, len(drills))
	copy(cp, drills)
	return &Set{drills: cp, byID: byID}, nil
}

// All returns a copy of every drill in the set.
func (s *Set) All() []Drill {
	out := make([]Drill, len(s.drills))
	copy(out, s.drills)
	return out
}

// Len reports how many drills the set holds.
func (s *Set) Len() int { return len(s.drills) }

// ByID looks up a single drill.
func (s *Set) ByID(id string) (Drill, bool) {
	i, ok := s.byID[id]
	if !ok {
		return Drill{}, false
	}
	return s.drills[i], true
}

// Topics lists the distinct topics present, sorted.
func (s *Set) Topics() []string {
	seen := map[string]bool{}
	for _, d := range s.drills {
		seen[d.Topic] = true
	}
	out := make([]string, 0, len(seen))
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Filter returns the drills matching every non-empty criterion.
func (s *Set) Filter(topic string, kind Kind, diff Difficulty) []Drill {
	var out []Drill
	for _, d := range s.drills {
		if topic != "" && !strings.EqualFold(d.Topic, topic) {
			continue
		}
		if kind != "" && d.Kind != kind {
			continue
		}
		if diff != "" && d.Difficulty != diff {
			continue
		}
		out = append(out, d)
	}
	return out
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
