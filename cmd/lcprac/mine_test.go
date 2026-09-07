package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
)

// The example is what a new author copies, so it has to be a deck that loads.
func TestExampleFileIsAValidDeck(t *testing.T) {
	var ds []drill.Drill
	if err := json.Unmarshal([]byte(exampleFile), &ds); err != nil {
		t.Fatalf("example does not parse: %v", err)
	}
	if len(ds) < 2 {
		t.Fatalf("want at least 2 example drills, got %d", len(ds))
	}
	builtin, err := drill.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := drill.Merge(builtin, ds); err != nil {
		t.Fatalf("example does not merge into the builtin deck: %v", err)
	}
	for _, d := range ds {
		if _, ok := builtin.ByID(d.ID); ok {
			t.Errorf("example drill %s shadows a builtin drill", d.ID)
		}
	}
}

func TestRemoveMineTombstonesAndReportsAgain(t *testing.T) {
	dir := t.TempDir()
	stamp := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	ds := []drill.Drill{{
		ID: "mine-doomed", Title: "t", Kind: drill.KindRecall, Topic: "arrays",
		Difficulty: drill.Easy, EstMinutes: 2, Prompt: "p", Answer: "a",
		Explanation: "e", UpdatedAt: &stamp,
	}}
	b, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := removeMine(&out, dir, "mine-doomed"); err != nil {
		t.Fatalf("removeMine: %v", err)
	}
	if !strings.Contains(out.String(), "deleted mine-doomed") {
		t.Errorf("output = %q, want it to name the deleted drill", out.String())
	}
	live, err := drill.LoadUserDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(live) != 0 {
		t.Fatalf("deck still holds %+v", live)
	}

	// Deleting it twice is not an error: the second time it is already gone.
	out.Reset()
	if err := removeMine(&out, dir, "mine-doomed"); err != nil {
		t.Fatalf("second removeMine: %v", err)
	}
	if !strings.Contains(out.String(), "already deleted") {
		t.Errorf("output = %q, want it to say the drill was already deleted", out.String())
	}
	// A builtin id is not yours to delete, and neither is one that never was.
	if err := removeMine(&out, dir, "two-pointers-invariant"); err == nil {
		t.Error("deleting a drill that is not yours was accepted")
	}
}
