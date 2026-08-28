package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

func notedStore(t *testing.T, notes map[string]string) *progress.Store {
	t.Helper()
	store := progress.New("")
	for id, n := range notes {
		store.SetNote(id, n)
	}
	return store
}

func TestListNotesEmpty(t *testing.T) {
	set, err := drill.Builtin()
	if err != nil {
		t.Fatalf("Builtin: %v", err)
	}
	out := &bytes.Buffer{}
	listNotes(out, set, progress.New(""))
	if !strings.Contains(out.String(), "no notes yet") {
		t.Fatalf("expected the empty hint, got %q", out.String())
	}
}

// A note on a drill that is no longer in the deck still lists: the note is
// yours, and losing it silently would be worse than a missing title.
func TestListNotesIncludesUnknownDrill(t *testing.T) {
	set, err := drill.Builtin()
	if err != nil {
		t.Fatalf("Builtin: %v", err)
	}
	known := set.All()[0]
	store := notedStore(t, map[string]string{known.ID: "left pointer never rewinds", "gone-drill": "kept anyway"})
	out := &bytes.Buffer{}
	listNotes(out, set, store)

	got := out.String()
	for _, want := range []string{known.ID, known.Title, "left pointer never rewinds", "gone-drill", "kept anyway", "2 note(s)"} {
		if !strings.Contains(got, want) {
			t.Errorf("listNotes output missing %q:\n%s", want, got)
		}
	}
}

func TestNotesForBuildsIDMap(t *testing.T) {
	store := notedStore(t, map[string]string{"a": "one", "b": "two"})
	store.Record("c", true, time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC))

	notes := notesFor(store)
	if len(notes) != 2 || notes["a"] != "one" || notes["b"] != "two" {
		t.Fatalf("notesFor = %v", notes)
	}
	if _, ok := notes["c"]; ok {
		t.Fatalf("a drill with no note should not appear: %v", notes)
	}
}

// review is where a note earns its keep: the drill you keep missing comes with
// the wording that worked for you last time.
func TestWriteReviewShowsNote(t *testing.T) {
	d := drill.Drill{ID: "x", Title: "T", Topic: "hashmap", Kind: drill.KindRecall, Prompt: "p", Answer: "a", Explanation: "e"}
	out := &bytes.Buffer{}
	writeReview(out, d, progress.Record{DrillID: "x", Note: "count first, then scan"}, false)

	got := out.String()
	if !strings.Contains(got, "your note:") || !strings.Contains(got, "count first, then scan") {
		t.Fatalf("review missing the note:\n%s", got)
	}
}

func TestWriteReviewOmitsEmptyNote(t *testing.T) {
	d := drill.Drill{ID: "x", Title: "T", Topic: "hashmap", Kind: drill.KindRecall, Prompt: "p", Answer: "a", Explanation: "e"}
	out := &bytes.Buffer{}
	writeReview(out, d, progress.Record{DrillID: "x"}, false)
	if strings.Contains(out.String(), "your note") {
		t.Fatalf("unexpected note heading:\n%s", out.String())
	}
}
