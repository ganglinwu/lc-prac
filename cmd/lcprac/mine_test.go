package main

import (
	"encoding/json"
	"testing"

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
