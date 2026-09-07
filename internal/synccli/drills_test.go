package synccli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
)

func ownDrill(id, prompt string, stamp time.Time) drill.Drill {
	d := drill.Drill{
		ID: id, Title: "t " + id, Kind: drill.KindRecall, Topic: "arrays",
		Difficulty: drill.Easy, EstMinutes: 2, Prompt: prompt,
		Answer: "a", Explanation: "e",
	}
	if !stamp.IsZero() {
		d.UpdatedAt = &stamp
	}
	return d
}

// machine is one machine's drills directory.
func machine(t *testing.T, ds ...drill.Drill) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "drills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if len(ds) > 0 {
		b, err := json.MarshalIndent(ds, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "mine.json"), append(b, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func loaded(t *testing.T, dir string) map[string]drill.Drill {
	t.Helper()
	ds, err := drill.LoadUserDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]drill.Drill{}
	for _, d := range ds {
		out[d.ID] = d
	}
	return out
}

func TestSyncDrillsCarriesOwnDrillsBetweenMachines(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: testToken})
	stamp := time.Now().UTC().Truncate(time.Second)

	laptop := machine(t, ownDrill("laptop-idea", "what does lps[i] mean", stamp))
	if n, u, err := SyncDrills(c, laptop, true); err != nil || n != 0 || u != 0 {
		t.Fatalf("first push: new=%d updated=%d err=%v", n, u, err)
	}

	desktop := machine(t)
	n, u, err := SyncDrills(c, desktop, true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || u != 0 {
		t.Fatalf("desktop got new=%d updated=%d, want 1 and 0", n, u)
	}
	got := loaded(t, desktop)
	if d, ok := got["laptop-idea"]; !ok || d.Prompt != "what does lps[i] mean" {
		t.Fatalf("desktop deck = %+v, want the laptop's drill", got)
	}

	// A second sync on either side must bring nothing back.
	if n, u, err := SyncDrills(c, desktop, true); err != nil || n != 0 || u != 0 {
		t.Fatalf("re-sync: new=%d updated=%d err=%v", n, u, err)
	}
	if n, u, err := SyncDrills(c, laptop, true); err != nil || n != 0 || u != 0 {
		t.Fatalf("laptop re-sync: new=%d updated=%d err=%v", n, u, err)
	}
}

func TestSyncDrillsCarriesAnEdit(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: testToken})
	stamp := time.Now().UTC().Truncate(time.Second)

	laptop := machine(t, ownDrill("shared", "first wording", stamp))
	if _, _, err := SyncDrills(c, laptop, true); err != nil {
		t.Fatal(err)
	}
	desktop := machine(t, ownDrill("shared", "second wording", stamp.Add(time.Hour)))
	if _, _, err := SyncDrills(c, desktop, true); err != nil {
		t.Fatal(err)
	}

	n, u, err := SyncDrills(c, laptop, true)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || u != 1 {
		t.Fatalf("laptop got new=%d updated=%d, want 0 and 1", n, u)
	}
	if got := loaded(t, laptop)["shared"].Prompt; got != "second wording" {
		t.Fatalf("laptop kept %q, want the newer edit", got)
	}
	// The edit lands in the file that already held the drill, not a new one.
	if _, err := os.Stat(filepath.Join(laptop, drill.SyncedFile)); !os.IsNotExist(err) {
		t.Error("an edit to an existing drill created synced.json")
	}
}

func TestSyncDrillsDryRunWritesNothing(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: testToken})
	stamp := time.Now().UTC().Truncate(time.Second)

	laptop := machine(t, ownDrill("laptop-idea", "p", stamp))
	if _, _, err := SyncDrills(c, laptop, true); err != nil {
		t.Fatal(err)
	}
	desktop := machine(t)
	n, u, err := SyncDrills(c, desktop, false)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || u != 0 {
		t.Fatalf("dry run reported new=%d updated=%d, want 1 and 0", n, u)
	}
	if got := loaded(t, desktop); len(got) != 0 {
		t.Fatalf("dry run wrote %d drills", len(got))
	}
}

func TestSyncDrillsWithNoDrillsAnywhere(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: testToken})
	dir := filepath.Join(t.TempDir(), "never-created")
	n, u, err := SyncDrills(c, dir, true)
	if err != nil {
		t.Fatalf("a machine with no drills of its own: %v", err)
	}
	if n != 0 || u != 0 {
		t.Fatalf("new=%d updated=%d, want nothing", n, u)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("syncing no drills created the directory")
	}
}

func TestSyncDrillsSurfacesServerError(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: "wrong-token-0123456789"})
	if _, _, err := SyncDrills(c, machine(t), true); err == nil {
		t.Fatal("want an error for a bad token")
	}
}

func TestDiffDrillsCounts(t *testing.T) {
	stamp := time.Now().UTC().Truncate(time.Second)
	local := []drill.Drill{ownDrill("a", "p", stamp), ownDrill("b", "q", stamp)}
	merged := []drill.Drill{
		ownDrill("a", "p", stamp),
		ownDrill("b", "q edited", stamp.Add(time.Minute)),
		ownDrill("c", "r", stamp),
	}
	if n, u := DiffDrills(local, merged); n != 1 || u != 1 {
		t.Fatalf("new=%d updated=%d, want 1 and 1", n, u)
	}
}
