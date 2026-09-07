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
	if n, u, _, err := SyncDrills(c, laptop, true); err != nil || n != 0 || u != 0 {
		t.Fatalf("first push: new=%d updated=%d err=%v", n, u, err)
	}

	desktop := machine(t)
	n, u, _, err := SyncDrills(c, desktop, true)
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
	if n, u, _, err := SyncDrills(c, desktop, true); err != nil || n != 0 || u != 0 {
		t.Fatalf("re-sync: new=%d updated=%d err=%v", n, u, err)
	}
	if n, u, _, err := SyncDrills(c, laptop, true); err != nil || n != 0 || u != 0 {
		t.Fatalf("laptop re-sync: new=%d updated=%d err=%v", n, u, err)
	}
}

func TestSyncDrillsCarriesAnEdit(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: testToken})
	stamp := time.Now().UTC().Truncate(time.Second)

	laptop := machine(t, ownDrill("shared", "first wording", stamp))
	if _, _, _, err := SyncDrills(c, laptop, true); err != nil {
		t.Fatal(err)
	}
	desktop := machine(t, ownDrill("shared", "second wording", stamp.Add(time.Hour)))
	if _, _, _, err := SyncDrills(c, desktop, true); err != nil {
		t.Fatal(err)
	}

	n, u, _, err := SyncDrills(c, laptop, true)
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
	if _, _, _, err := SyncDrills(c, laptop, true); err != nil {
		t.Fatal(err)
	}
	desktop := machine(t)
	n, u, _, err := SyncDrills(c, desktop, false)
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
	n, u, _, err := SyncDrills(c, dir, true)
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
	if _, _, _, err := SyncDrills(c, machine(t), true); err == nil {
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
	if n, u, _ := DiffDrills(local, merged); n != 1 || u != 1 {
		t.Fatalf("new=%d updated=%d, want 1 and 1", n, u)
	}
}

func ownTombstone(id string, stamp time.Time) drill.Drill {
	return drill.Drill{ID: id, DeletedAt: &stamp}
}

// A delete has to survive a round trip through the server, or the next pull
// would hand the drill straight back to the machine that deleted it.
func TestSyncDrillsCarriesADelete(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: testToken})
	stamp := time.Now().UTC().Truncate(time.Second)
	laptop := machine(t, ownDrill("keep", "p", stamp), ownDrill("drop", "q", stamp))
	desktop := machine(t)

	if _, _, _, err := SyncDrills(c, laptop, true); err != nil {
		t.Fatal(err)
	}
	if n, _, _, err := SyncDrills(c, desktop, true); err != nil || n != 2 {
		t.Fatalf("desktop pulled %d drills, err %v; want 2", n, err)
	}

	files, found, err := drill.DeleteDrill(laptop, "drop", stamp.Add(time.Minute))
	if err != nil || !found || files != 1 {
		t.Fatalf("DeleteDrill = %d, %v, %v", files, found, err)
	}
	if _, _, d, err := SyncDrills(c, laptop, true); err != nil || d != 0 {
		t.Fatalf("laptop push reported %d deletes from elsewhere, err %v; want 0", d, err)
	}
	if _, _, d, err := SyncDrills(c, desktop, true); err != nil || d != 1 {
		t.Fatalf("desktop got %d deletes, err %v; want 1", d, err)
	}
	if got := loaded(t, desktop); len(got) != 1 || got["drop"].ID != "" {
		t.Fatalf("desktop deck = %+v, want only the kept drill", got)
	}

	// The deleted drill must not come back to either machine, however many
	// times they sync.
	for _, dir := range []string{laptop, desktop} {
		if n, u, d, err := SyncDrills(c, dir, true); err != nil || n != 0 || u != 0 || d != 0 {
			t.Fatalf("resync of %s = %d/%d/%d, err %v; want no change", dir, n, u, d, err)
		}
		if got := loaded(t, dir); len(got) != 1 || got["keep"].ID != "keep" {
			t.Fatalf("deck at %s = %+v, want only the kept drill", dir, got)
		}
	}
}

// A delete on one machine loses to a later rewrite on the other, so the drill
// you rewrote on purpose is not eaten by yesterday's deletion.
func TestSyncDrillsLetsALaterRewriteBeatADelete(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: testToken})
	stamp := time.Now().UTC().Truncate(time.Second)
	laptop := machine(t, ownTombstone("a", stamp))
	desktop := machine(t, ownDrill("a", "written again", stamp.Add(time.Minute)))

	if _, _, _, err := SyncDrills(c, laptop, true); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := SyncDrills(c, desktop, true); err != nil {
		t.Fatal(err)
	}
	// The rewrite lands on the tombstone already sitting in the laptop's
	// file, so it reads as an update rather than as a new drill.
	if _, u, _, err := SyncDrills(c, laptop, true); err != nil || u != 1 {
		t.Fatalf("laptop pulled %d updates back, err %v; want the rewrite", u, err)
	}
	if got := loaded(t, laptop); got["a"].Prompt != "written again" {
		t.Fatalf("laptop deck = %+v, want the rewritten drill", got)
	}
}

// A dry run must count a delete without performing it.
func TestSyncDrillsDryRunCountsADeleteWithoutApplyingIt(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: testToken})
	stamp := time.Now().UTC().Truncate(time.Second)
	laptop := machine(t, ownTombstone("a", stamp.Add(time.Minute)))
	desktop := machine(t, ownDrill("a", "p", stamp))

	if _, _, _, err := SyncDrills(c, laptop, true); err != nil {
		t.Fatal(err)
	}
	if _, _, d, err := SyncDrills(c, desktop, false); err != nil || d != 1 {
		t.Fatalf("dry run reported %d deletes, err %v; want 1", d, err)
	}
	if got := loaded(t, desktop); len(got) != 1 {
		t.Fatalf("dry run changed the deck to %+v", got)
	}
}
