package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

// answers joins interview replies into the stdin an asker reads.
func answers(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

func testDeck(t *testing.T) *drill.Set {
	t.Helper()
	set, err := drill.Builtin()
	if err != nil {
		t.Fatalf("builtin: %v", err)
	}
	return set
}

// readBack loads the file addDrill wrote and returns the drills in it.
func readBack(t *testing.T, path string) []drill.Drill {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var got []drill.Drill
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return got
}

func TestAddDrillWritesValidRecallDrill(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mine.json")
	in := answers(
		"recall",
		"Union find path compression",
		"graphs",
		"easy",
		"Why does find() reassign parent[x] on the way up?",
		"It flattens the path so later finds are near-constant time.",
		"Path compression plus union by rank gives inverse-Ackermann amortised cost.",
		"3",
		"", // no hints
		"LC 547",
		"", // stop refs
		"", // accept generated id
		"y",
	)
	var out bytes.Buffer
	if err := addDrill(strings.NewReader(in), &out, testDeck(t), path, problemPrefill{}); err != nil {
		t.Fatalf("addDrill: %v", err)
	}
	got := readBack(t, path)
	if len(got) != 1 {
		t.Fatalf("wrote %d drills, want 1", len(got))
	}
	d := got[0]
	if d.ID != "mine-union-find-path-compression" {
		t.Errorf("id = %q", d.ID)
	}
	if d.Kind != drill.KindRecall || d.Topic != "graphs" || d.EstMinutes != 3 {
		t.Errorf("unexpected drill: %+v", d)
	}
	if len(d.Refs) != 1 || d.Refs[0] != "LC 547" {
		t.Errorf("refs = %v", d.Refs)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("written drill does not validate: %v", err)
	}
}

func TestAddDrillChoiceTakesAnswerByNumber(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mine.json")
	in := answers(
		"choice",
		"Heap for top k",
		"heap",
		"medium",
		"Which heap do you keep for the k largest elements?",
		"a min-heap of size k",
		"a max-heap of size k",
		"", // done adding choices
		"1",
		"Evicting the smallest keeps the k largest, so the root is the kth largest.",
		"2",
		"", "", "",
		"y",
	)
	var out bytes.Buffer
	if err := addDrill(strings.NewReader(in), &out, testDeck(t), path, problemPrefill{}); err != nil {
		t.Fatalf("addDrill: %v", err)
	}
	d := readBack(t, path)[0]
	if len(d.Choices) != 2 || d.Answer != "a min-heap of size k" {
		t.Fatalf("choices = %v answer = %q", d.Choices, d.Answer)
	}
	if err := d.Validate(); err != nil {
		t.Errorf("validate: %v", err)
	}
}

func TestAddDrillRepromptsOnBadInput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mine.json")
	in := answers(
		"quiz",   // not a kind
		"recall", // retry
		"",       // empty title
		"Two pointer shrink",
		"two-pointers",
		"brutal", // not a difficulty
		"easy",
		"When do you move the left pointer?",
		"When the window is invalid.",
		"Shrinking only on invalidity keeps each pointer monotone.",
		"99", // out of range minutes
		"2",
		"", "", "",
		"y",
	)
	var out bytes.Buffer
	if err := addDrill(strings.NewReader(in), &out, testDeck(t), path, problemPrefill{}); err != nil {
		t.Fatalf("addDrill: %v", err)
	}
	text := out.String()
	for _, want := range []string{"pick one of: recall", "title cannot be empty", "1 to 10"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q\n%s", want, text)
		}
	}
	if got := readBack(t, path)[0]; got.EstMinutes != 2 || got.Difficulty != drill.Easy {
		t.Errorf("bad values survived reprompt: %+v", got)
	}
}

func TestAddDrillDeclineWritesNothing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mine.json")
	in := answers(
		"complexity", "Binary search cost", "binary-search", "easy",
		"Halving a sorted range until it is empty. Time?",
		"O(log n)",
		"Each step throws away half the range.",
		"1", "", "", "",
		"n",
	)
	err := addDrill(strings.NewReader(in), &bytes.Buffer{}, testDeck(t), path, problemPrefill{})
	if !errors.Is(err, errCancelled) {
		t.Fatalf("err = %v, want errCancelled", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Errorf("file written despite declining")
	}
}

func TestAddDrillCancelsOnEOF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mine.json")
	err := addDrill(strings.NewReader("recall\n"), &bytes.Buffer{}, testDeck(t), path, problemPrefill{})
	if !errors.Is(err, errCancelled) {
		t.Fatalf("err = %v, want errCancelled", err)
	}
}

func TestAppendDrillKeepsExistingAndIsLoadable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mine.json")
	first := drill.Drill{
		ID: "mine-a", Title: "A", Kind: drill.KindRecall, Topic: "stack",
		Difficulty: drill.Easy, EstMinutes: 1, Prompt: "p", Answer: "a", Explanation: "e",
	}
	second := first
	second.ID, second.Title = "mine-b", "B"
	if err := appendDrill(path, first); err != nil {
		t.Fatalf("first append: %v", err)
	}
	if err := appendDrill(path, second); err != nil {
		t.Fatalf("second append: %v", err)
	}
	loaded, err := drill.LoadUserDir(dir)
	if err != nil {
		t.Fatalf("LoadUserDir: %v", err)
	}
	if len(loaded) != 2 || loaded[0].ID != "mine-a" || loaded[1].ID != "mine-b" {
		t.Fatalf("loaded = %+v", loaded)
	}
	if _, err := drill.Merge(testDeck(t), loaded); err != nil {
		t.Errorf("appended drills do not merge into the deck: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("temp file left behind: %v", entries)
	}
}

func TestAppendDrillRefusesBrokenFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mine.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := appendDrill(path, drill.Drill{ID: "mine-x"})
	if err == nil || !strings.Contains(err.Error(), "mine.json") {
		t.Fatalf("err = %v, want one naming the file", err)
	}
	if raw, _ := os.ReadFile(path); string(raw) != "{not json" {
		t.Errorf("broken file was overwritten: %q", raw)
	}
}

func TestSlugifyAndUniqueID(t *testing.T) {
	cases := map[string]string{
		"Two Sum: the hashmap trick": "mine-two-sum-the-hashmap-trick",
		"  spaced  out  ":            "mine-spaced-out",
		"!!!":                        "mine-drill",
		"a b c d e f g h":            "mine-a-b-c-d-e-f",
	}
	for title, want := range cases {
		if got := slugify(title); got != want {
			t.Errorf("slugify(%q) = %q, want %q", title, got, want)
		}
	}
	deck := testDeck(t)
	taken := deck.All()[0].ID
	if got := uniqueID(taken, deck); got != taken+"-2" {
		t.Errorf("uniqueID(%q) = %q, want %q", taken, got, taken+"-2")
	}
}

func TestPrefillForDeckProblem(t *testing.T) {
	set, store := problemFixture(t)
	pre, err := prefillFor(set, store, "20")
	if err != nil {
		t.Fatalf("prefillFor: %v", err)
	}
	if pre.Ref != "LC 20 Valid Parentheses" {
		t.Errorf("ref = %q", pre.Ref)
	}
	if pre.Title != "Valid Parentheses" {
		t.Errorf("title = %q", pre.Title)
	}
	if pre.Topic != "stack" || !pre.Covered {
		t.Errorf("topic = %q, covered = %v; want stack, true", pre.Topic, pre.Covered)
	}
}

func TestPrefillForAttemptOnlyProblemIsAGap(t *testing.T) {
	set, store := problemFixture(t)
	store.LogAttempt("LC 261 Graph Valid Tree", progress.Failed, "", time.Now())
	pre, err := prefillFor(set, store, "261")
	if err != nil {
		t.Fatalf("prefillFor: %v", err)
	}
	if pre.Ref != "LC 261 Graph Valid Tree" || pre.Covered {
		t.Errorf("pre = %+v, want the logged ref marked uncovered", pre)
	}
	if pre.Topic != "" {
		t.Errorf("topic = %q, want empty: no drill says what topic it is", pre.Topic)
	}
}

func TestPrefillForUnknownProblemIsNormalisedNotAnError(t *testing.T) {
	set, store := problemFixture(t)
	pre, err := prefillFor(set, store, "424 Longest Repeating Character Replacement")
	if err != nil {
		t.Fatalf("prefillFor: %v", err)
	}
	if pre.Ref != "LC 424 Longest Repeating Character Replacement" || pre.Covered {
		t.Errorf("pre = %+v", pre)
	}
}

func TestPrefillForAmbiguousTitleFails(t *testing.T) {
	set, store := problemFixture(t)
	if _, err := prefillFor(set, store, "lc"); err == nil {
		t.Fatal("want an error for a query that names nothing")
	}
	if _, err := prefillFor(set, store, "LC"); err == nil {
		t.Fatal("want an error for a query that names nothing")
	}
	if _, err := prefillFor(set, store, "a"); err == nil {
		t.Fatal("want an error for an ambiguous title query")
	}
}

func TestAddDrillWithPrefillCarriesRefAndDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mine.json")
	pre := problemPrefill{Ref: "LC 261 Graph Valid Tree", Title: "Graph Valid Tree"}
	in := answers(
		"recall",
		"", // accept the prefilled title
		"graphs",
		"medium",
		"When is an undirected graph a valid tree?",
		"n-1 edges and fully connected.",
		"Either check for a cycle with union find, or count nodes reached from 0.",
		"2",
		"", // no hints
		"", // no extra refs
		"", // accept generated id
		"y",
	)
	var out bytes.Buffer
	if err := addDrill(strings.NewReader(in), &out, testDeck(t), path, pre); err != nil {
		t.Fatalf("addDrill: %v", err)
	}
	got := readBack(t, path)
	if len(got) != 1 {
		t.Fatalf("wrote %d drills, want 1", len(got))
	}
	d := got[0]
	if d.Title != "Graph Valid Tree" {
		t.Errorf("title = %q, want the prefilled default", d.Title)
	}
	if len(d.Refs) != 1 || d.Refs[0] != pre.Ref {
		t.Errorf("refs = %v, want the problem carried through", d.Refs)
	}
	text := out.String()
	for _, want := range []string{"for LC 261 Graph Valid Tree (nothing in the deck covers it yet)", "ref 1: LC 261 Graph Valid Tree", "lcprac drill -problem 261"} {
		if !strings.Contains(text, want) {
			t.Errorf("output missing %q\n%s", want, text)
		}
	}
}
