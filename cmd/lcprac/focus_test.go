package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

func focusDeck(t *testing.T) *drill.Set {
	t.Helper()
	mk := func(id, topic string) drill.Drill {
		return drill.Drill{
			ID: id, Title: id, Topic: topic, Kind: drill.KindRecall,
			Difficulty: drill.Easy, Prompt: "p?", Answer: "a",
			Explanation: "because", EstMinutes: 2,
		}
	}
	set, err := drill.NewSet([]drill.Drill{
		mk("dp-1", "dp"), mk("dp-2", "dp"),
		mk("gr-1", "graphs"), mk("gr-2", "graphs"), mk("gr-3", "graphs"),
	})
	if err != nil {
		t.Fatalf("NewSet: %v", err)
	}
	return set
}

func focusStore(t *testing.T) *progress.Store {
	t.Helper()
	store, err := progress.Load(filepath.Join(t.TempDir(), "progress.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return store
}

func TestTopicStatsSummarisesHistory(t *testing.T) {
	set, store := focusDeck(t), focusStore(t)
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	store.RecordOutcome("dp-1", progress.Missed, now)
	store.RecordOutcome("dp-1", progress.Missed, now)
	store.RecordOutcome("dp-1", progress.Assisted, now)

	rows := topicStats(set, store, now)
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	dp := rows[0]
	if dp.Topic != "dp" || dp.Total != 2 || dp.Tried != 1 || dp.Seen != 3 || dp.Correct != 1 || dp.Assisted != 1 {
		t.Errorf("dp row = %+v", dp)
	}
	// A never-seen drill counts as due, so an untried topic is all due.
	if rows[1].Seen != 0 || rows[1].Tried != 0 || rows[1].Due != 3 {
		t.Errorf("graphs row = %+v, want untried and all due", rows[1])
	}
}

func TestWeakTopicExplainsThePick(t *testing.T) {
	set, store := focusDeck(t), focusStore(t)
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		store.RecordOutcome("dp-1", progress.Missed, now)
	}
	topic, why := weakTopic(set, store, now)
	if topic != "dp" {
		t.Fatalf("topic = %q, want dp", topic)
	}
	if !strings.Contains(why, "0%") || !strings.Contains(why, "3 attempts") {
		t.Errorf("why = %q, want the accuracy and attempt count", why)
	}
}

// With no graded attempts the pick falls back to coverage, and must say so
// rather than claiming an accuracy it does not have.
func TestWeakTopicFallsBackToUntried(t *testing.T) {
	set, store := focusDeck(t), focusStore(t)
	topic, why := weakTopic(set, store, time.Now())
	if topic != "graphs" {
		t.Fatalf("topic = %q, want graphs", topic)
	}
	if !strings.Contains(why, "least practised") || !strings.Contains(why, "3 of 3") {
		t.Errorf("why = %q", why)
	}
}

func TestLeechSetKeepsOnlyLeeches(t *testing.T) {
	set, store := focusDeck(t), focusStore(t)
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < progress.LeechMisses; i++ {
		store.RecordOutcome("gr-2", progress.Missed, now)
	}
	store.RecordOutcome("dp-1", progress.Missed, now)

	sub, err := leechSet(set, store)
	if err != nil {
		t.Fatalf("leechSet: %v", err)
	}
	if sub == nil || sub.Len() != 1 {
		t.Fatalf("leechSet = %v, want one drill", sub)
	}
	if _, ok := sub.ByID("gr-2"); !ok {
		t.Errorf("subset = %v, want gr-2", sub.All())
	}
}

// A record whose drill is gone (a user file deleted between sittings) must not
// break the mode; history outlives the deck.
func TestLeechSetIgnoresUnknownDrills(t *testing.T) {
	set, store := focusDeck(t), focusStore(t)
	now := time.Now()
	for i := 0; i < progress.LeechMisses; i++ {
		store.RecordOutcome("deleted-drill", progress.Missed, now)
	}
	sub, err := leechSet(set, store)
	if err != nil {
		t.Fatalf("leechSet: %v", err)
	}
	if sub != nil {
		t.Errorf("leechSet = %v, want nil", sub.All())
	}
}

func TestNarrowDeckLeechAndWeak(t *testing.T) {
	set, store := focusDeck(t), focusStore(t)
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	for i := 0; i < progress.LeechMisses; i++ {
		store.RecordOutcome("gr-2", progress.Missed, now)
	}
	out := &bytes.Buffer{}
	got, topic, err := narrowDeck(set, store, now, out, true, true, "")
	if err != nil {
		t.Fatalf("narrowDeck: %v", err)
	}
	if got.Len() != 1 {
		t.Errorf("deck = %d drills, want 1", got.Len())
	}
	// -weak reads the narrowed deck, so the only topic left is the pick.
	if topic != "graphs" {
		t.Errorf("topic = %q, want graphs", topic)
	}
	if !strings.Contains(out.String(), "keep missing") {
		t.Errorf("output = %q", out.String())
	}
}

// An empty history must not block practice: both shortcuts degrade to a normal
// session and say why.
func TestNarrowDeckFallsBackWithNoHistory(t *testing.T) {
	set, store := focusDeck(t), focusStore(t)
	out := &bytes.Buffer{}
	got, topic, err := narrowDeck(set, store, time.Now(), out, true, false, "")
	if err != nil {
		t.Fatalf("narrowDeck: %v", err)
	}
	if got.Len() != set.Len() || topic != "" {
		t.Errorf("deck = %d drills, topic = %q, want the full deck untouched", got.Len(), topic)
	}
	if !strings.Contains(out.String(), "normal session") {
		t.Errorf("output = %q", out.String())
	}
}

func TestNarrowDeckKeepsExplicitTopic(t *testing.T) {
	set, store := focusDeck(t), focusStore(t)
	out := &bytes.Buffer{}
	_, topic, err := narrowDeck(set, store, time.Now(), out, false, true, "dp")
	if err != nil {
		t.Fatalf("narrowDeck: %v", err)
	}
	if topic != "dp" {
		t.Errorf("topic = %q, want the explicit dp", topic)
	}
	if !strings.Contains(out.String(), "-weak ignored") {
		t.Errorf("output = %q", out.String())
	}
}

func refDeck(t *testing.T) *drill.Set {
	t.Helper()
	mk := func(id, topic string, refs ...string) drill.Drill {
		return drill.Drill{
			ID: id, Title: id, Topic: topic, Kind: drill.KindRecall,
			Difficulty: drill.Easy, Prompt: "p?", Answer: "a",
			Explanation: "because", EstMinutes: 2, Refs: refs,
		}
	}
	set, err := drill.NewSet([]drill.Drill{
		mk("iv-1", "intervals", "LC 56 Merge Intervals"),
		mk("iv-2", "intervals", "LC 56 Merge Intervals", "LC 435 Non-overlapping Intervals"),
		mk("bs-1", "binary-search", "LC 34 Find First and Last Position"),
		mk("bs-2", "binary-search", "LC 340 Longest Substring with At Most K Distinct"),
		mk("cx-1", "complexity"),
	})
	if err != nil {
		t.Fatalf("NewSet: %v", err)
	}
	return set
}

func TestMatchesProblem(t *testing.T) {
	cases := []struct {
		ref, query string
		want       bool
	}{
		{"LC 56 Merge Intervals", "56", true},
		{"LC 56 Merge Intervals", "lc 56", true},
		{"LC 56 Merge Intervals", "LC56", true},
		{"LC 340 Longest Substring", "34", false},
		{"LC 34 Find First and Last", "34", true},
		{"LC 56 Merge Intervals", "merge", true},
		{"LC 56 Merge Intervals", "MERGE INTERVALS", true},
		{"LC 56 Merge Intervals", "islands", false},
		{"LC 56 Merge Intervals", "  ", false},
	}
	for _, c := range cases {
		if got := matchesProblem(c.ref, c.query); got != c.want {
			t.Errorf("matchesProblem(%q, %q) = %v, want %v", c.ref, c.query, got, c.want)
		}
	}
}

func TestProblemDeckByNumber(t *testing.T) {
	var buf bytes.Buffer
	sub, err := problemDeck(refDeck(t), "56", &buf)
	if err != nil {
		t.Fatalf("problemDeck: %v", err)
	}
	if sub.Len() != 2 {
		t.Fatalf("got %d drills, want 2", sub.Len())
	}
	for _, id := range []string{"iv-1", "iv-2"} {
		if _, ok := sub.ByID(id); !ok {
			t.Errorf("missing %s", id)
		}
	}
	if out := buf.String(); !strings.Contains(out, "LC 56 Merge Intervals") || !strings.Contains(out, "2 drill(s)") {
		t.Errorf("report did not name the problem and count: %q", out)
	}
}

// A drill counts once even when several of its refs match, and a match on a
// second ref is enough to pull it in.
func TestProblemDeckByTitle(t *testing.T) {
	var buf bytes.Buffer
	sub, err := problemDeck(refDeck(t), "intervals", &buf)
	if err != nil {
		t.Fatalf("problemDeck: %v", err)
	}
	if sub.Len() != 2 {
		t.Fatalf("got %d drills, want 2", sub.Len())
	}
	if out := buf.String(); !strings.Contains(out, "LC 435") {
		t.Errorf("report should list every matched ref: %q", out)
	}
}

func TestProblemDeckNoMatch(t *testing.T) {
	var buf bytes.Buffer
	if _, err := problemDeck(refDeck(t), "999", &buf); err == nil {
		t.Fatal("want an error when no drill names the problem")
	} else if !strings.Contains(err.Error(), "lcprac problems") {
		t.Errorf("error should point at the problem list: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("nothing should be reported on a miss: %q", buf.String())
	}
}

func TestWarmUpFlag(t *testing.T) {
	if got := warmUpFlag(problemRow{Ref: "LC 56 Merge Intervals", Topics: []string{"intervals"}}); got != "-problem 56" {
		t.Errorf("got %q, want -problem 56", got)
	}
	if got := warmUpFlag(problemRow{Ref: "Blind 75 warmup", Topics: []string{"dp"}}); got != "-topic dp" {
		t.Errorf("got %q, want -topic dp", got)
	}
}

// The builtin deck has to actually answer a number, otherwise the flag is
// only usable against hand-written drills.
func TestProblemDeckBuiltin(t *testing.T) {
	set, err := drill.Builtin()
	if err != nil {
		t.Fatalf("Builtin: %v", err)
	}
	sub, err := problemDeck(set, "56", &bytes.Buffer{})
	if err != nil {
		t.Fatalf("problemDeck: %v", err)
	}
	if sub.Len() == 0 {
		t.Fatal("LC 56 should be backed by at least one builtin drill")
	}
}
