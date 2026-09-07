package drill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func mkDrill(id, prompt string, at *time.Time) Drill {
	return Drill{
		ID: id, Title: "t " + id, Kind: KindRecall, Topic: "arrays",
		Difficulty: Easy, EstMinutes: 2, Prompt: prompt,
		Answer: "a", Explanation: "e", UpdatedAt: at,
	}
}

func at(s string) *time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func TestEncodeDecodeDrillsRoundTrip(t *testing.T) {
	in := []Drill{mkDrill("a", "p", at("2026-01-02T03:04:05Z"))}
	b, err := EncodeDrills(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := DecodeDrills(b)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != "a" || !out[0].UpdatedAt.Equal(*in[0].UpdatedAt) {
		t.Fatalf("round trip lost data: %+v", out)
	}
	if out[0].Source != SourceUser {
		t.Errorf("decoded source = %q, want %q", out[0].Source, SourceUser)
	}
}

func TestDecodeDrillsEmptyAndInvalid(t *testing.T) {
	if ds, err := DecodeDrills(nil); err != nil || ds != nil {
		t.Fatalf("empty bytes: got %v, %v", ds, err)
	}
	bad, _ := json.Marshal([]Drill{{ID: "x", Title: "t"}})
	if _, err := DecodeDrills(bad); err == nil {
		t.Fatal("want an error for a drill that does not validate")
	}
	if _, err := DecodeDrills([]byte("not json")); err == nil {
		t.Fatal("want an error for junk")
	}
}

func TestMergeDrillsNewestEditWins(t *testing.T) {
	old := mkDrill("a", "old", at("2026-01-01T00:00:00Z"))
	recent := mkDrill("a", "new", at("2026-02-01T00:00:00Z"))
	for _, tc := range []struct {
		name          string
		local, remote []Drill
	}{
		{"remote newer", []Drill{old}, []Drill{recent}},
		{"local newer", []Drill{recent}, []Drill{old}},
	} {
		got := MergeDrills(tc.local, tc.remote)
		if len(got) != 1 || got[0].Prompt != "new" {
			t.Errorf("%s: got %+v, want the later edit", tc.name, got)
		}
	}
}

func TestMergeDrillsUnstampedLosesToStamped(t *testing.T) {
	got := MergeDrills([]Drill{mkDrill("a", "unstamped", nil)},
		[]Drill{mkDrill("a", "stamped", at("2020-01-01T00:00:00Z"))})
	if got[0].Prompt != "stamped" {
		t.Fatalf("got %q, want the stamped copy to win", got[0].Prompt)
	}
}

func TestMergeDrillsIsSymmetricAndSorted(t *testing.T) {
	a := mkDrill("b", "one", at("2026-01-01T00:00:00Z"))
	b := mkDrill("a", "two", at("2026-01-01T00:00:00Z"))
	clash := mkDrill("c", "left", nil)
	other := mkDrill("c", "right", nil)

	ab := MergeDrills([]Drill{a, clash}, []Drill{b, other})
	ba := MergeDrills([]Drill{b, other}, []Drill{a, clash})
	if len(ab) != 3 {
		t.Fatalf("got %d drills, want 3", len(ab))
	}
	if ab[0].ID != "a" || ab[1].ID != "b" || ab[2].ID != "c" {
		t.Errorf("not sorted by id: %v %v %v", ab[0].ID, ab[1].ID, ab[2].ID)
	}
	for i := range ab {
		if !SameDrill(ab[i], ba[i]) {
			t.Errorf("merge is not symmetric at %s: %q vs %q", ab[i].ID, ab[i].Prompt, ba[i].Prompt)
		}
	}
}

func TestMergeDrillsIdempotent(t *testing.T) {
	local := []Drill{mkDrill("a", "p", at("2026-01-01T00:00:00Z"))}
	once := MergeDrills(local, nil)
	twice := MergeDrills(once, once)
	if len(twice) != 1 || !SameDrill(once[0], twice[0]) {
		t.Fatalf("re-merging changed the result: %+v", twice)
	}
}

func TestSameDrillIgnoresSource(t *testing.T) {
	a := mkDrill("a", "p", at("2026-01-01T00:00:00Z"))
	b := a
	b.Source = SourceUser
	if !SameDrill(a, b) {
		t.Fatal("source should not count as an edit")
	}
	b.Prompt = "other"
	if SameDrill(a, b) {
		t.Fatal("a changed prompt should count as an edit")
	}
}

func writeJSONFile(t *testing.T, path string, ds []Drill) {
	t.Helper()
	b, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestApplyMergedUpdatesInPlaceAndFilesNewcomers(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "mine.json")
	writeJSONFile(t, mine, []Drill{
		mkDrill("kept", "unchanged", at("2026-01-01T00:00:00Z")),
		mkDrill("edited", "old text", at("2026-01-01T00:00:00Z")),
	})

	merged := []Drill{
		mkDrill("brand-new", "from elsewhere", at("2026-03-01T00:00:00Z")),
		mkDrill("edited", "new text", at("2026-02-01T00:00:00Z")),
		mkDrill("kept", "unchanged", at("2026-01-01T00:00:00Z")),
	}
	added, updated, err := ApplyMerged(dir, merged)
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 || updated != 1 {
		t.Fatalf("added=%d updated=%d, want 1 and 1", added, updated)
	}
	got, err := LoadUserDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]Drill{}
	for _, d := range got {
		byID[d.ID] = d
	}
	if len(byID) != 3 {
		t.Fatalf("deck has %d drills, want 3", len(byID))
	}
	if byID["edited"].Prompt != "new text" {
		t.Errorf("edited drill = %q, want the newer text", byID["edited"].Prompt)
	}
	// The edited drill must stay in the file it was organised into rather
	// than being duplicated into the synced file.
	inMine, err := readDrillFile(mine)
	if err != nil {
		t.Fatal(err)
	}
	if len(inMine) != 2 {
		t.Fatalf("mine.json has %d drills, want 2", len(inMine))
	}
	synced, err := readDrillFile(filepath.Join(dir, SyncedFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(synced) != 1 || synced[0].ID != "brand-new" {
		t.Fatalf("synced.json = %+v, want only the new drill", synced)
	}
}

func TestApplyMergedNoChangeLeavesFilesAlone(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "mine.json")
	ds := []Drill{mkDrill("a", "p", at("2026-01-01T00:00:00Z"))}
	writeJSONFile(t, mine, ds)
	before, err := os.ReadFile(mine)
	if err != nil {
		t.Fatal(err)
	}

	added, updated, err := ApplyMerged(dir, ds)
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 || updated != 0 {
		t.Fatalf("added=%d updated=%d, want no change", added, updated)
	}
	after, err := os.ReadFile(mine)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("an unchanged merge rewrote the file")
	}
	if _, err := os.Stat(filepath.Join(dir, SyncedFile)); !os.IsNotExist(err) {
		t.Error("an unchanged merge created synced.json")
	}
}

func TestApplyMergedIntoEmptyDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "drills")
	added, updated, err := ApplyMerged(dir, []Drill{mkDrill("a", "p", nil)})
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 || updated != 0 {
		t.Fatalf("added=%d updated=%d, want 1 and 0", added, updated)
	}
	got, err := LoadUserDir(dir)
	if err != nil || len(got) != 1 {
		t.Fatalf("loading a fresh dir gave %v, %v", got, err)
	}
}
