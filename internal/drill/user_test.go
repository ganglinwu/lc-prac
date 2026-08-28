package drill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const oneDrill = `[{"id":"%s","title":"T","kind":"recall","topic":"%s",
"difficulty":"easy","est_minutes":2,"prompt":"P","answer":"A","explanation":"E"}]`

func writeFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadUserDirMissingIsEmpty(t *testing.T) {
	ds, err := LoadUserDir(filepath.Join(t.TempDir(), "nope"))
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(ds) != 0 {
		t.Fatalf("want no drills, got %d", len(ds))
	}
}

func TestLoadUserDirReadsJSONOnly(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "a.json", strings.NewReplacer("%s", "mine-a").Replace(oneDrill))
	writeFile(t, dir, "notes.txt", "ignore me")
	ds, err := LoadUserDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ds) != 1 {
		t.Fatalf("want 1 drill, got %d", len(ds))
	}
	if ds[0].Source != SourceUser {
		t.Errorf("source = %q, want %q", ds[0].Source, SourceUser)
	}
}

func TestLoadUserDirNamesBadFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "broken.json", "{not json")
	_, err := LoadUserDir(dir)
	if err == nil || !strings.Contains(err.Error(), "broken.json") {
		t.Fatalf("error should name the file, got %v", err)
	}
}

func TestMergeAppendsAndReplaces(t *testing.T) {
	base, err := NewSet([]Drill{
		{ID: "a", Title: "A", Kind: KindRecall, Topic: "t", Difficulty: Easy, EstMinutes: 2, Prompt: "p", Answer: "old", Explanation: "e"},
		{ID: "b", Title: "B", Kind: KindRecall, Topic: "t", Difficulty: Easy, EstMinutes: 2, Prompt: "p", Answer: "a", Explanation: "e"},
	})
	if err != nil {
		t.Fatal(err)
	}
	merged, err := Merge(base, []Drill{
		{ID: "a", Title: "A2", Kind: KindRecall, Topic: "t", Difficulty: Easy, EstMinutes: 2, Prompt: "p", Answer: "new", Explanation: "e", Source: SourceUser},
		{ID: "c", Title: "C", Kind: KindRecall, Topic: "t", Difficulty: Easy, EstMinutes: 2, Prompt: "p", Answer: "a", Explanation: "e", Source: SourceUser},
	})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Len() != 3 {
		t.Fatalf("want 3 drills, got %d", merged.Len())
	}
	got, _ := merged.ByID("a")
	if got.Answer != "new" {
		t.Errorf("override did not take: answer = %q", got.Answer)
	}
	if all := merged.All(); all[0].ID != "a" || all[1].ID != "b" || all[2].ID != "c" {
		t.Errorf("replacement should keep its position, got %v", []string{all[0].ID, all[1].ID, all[2].ID})
	}
}

func TestMergeRejectsInvalidDrill(t *testing.T) {
	base, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Merge(base, []Drill{{ID: "bad"}}); err == nil {
		t.Fatal("want validation error for a malformed user drill")
	}
}

func TestUserDirFollowsEnv(t *testing.T) {
	t.Setenv("LCPRAC_HOME", "/tmp/lch")
	got, err := UserDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/lch", "drills"); got != want {
		t.Errorf("UserDir = %q, want %q", got, want)
	}
	t.Setenv("LCPRAC_HOME", "")
	t.Setenv("XDG_DATA_HOME", "/tmp/xdg")
	got, err = UserDir()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join("/tmp/xdg", "lcprac", "drills"); got != want {
		t.Errorf("UserDir = %q, want %q", got, want)
	}
}

func TestCombinedAddsUserDrills(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LCPRAC_HOME", home)
	builtin, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}

	set, n, err := Combined()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || set.Len() != builtin.Len() {
		t.Fatalf("empty home should give the builtin deck, got %d extra", n)
	}

	dir := filepath.Join(home, "drills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "mine.json", strings.NewReplacer("%s", "mine-x").Replace(oneDrill))
	set, n, err = Combined()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || set.Len() != builtin.Len()+1 {
		t.Fatalf("want 1 extra drill, got n=%d len=%d", n, set.Len())
	}
	if d, ok := set.ByID("mine-x"); !ok || d.Source != SourceUser {
		t.Errorf("user drill missing or unmarked: %v %v", ok, d.Source)
	}
}

func TestCombinedReportsBrokenFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LCPRAC_HOME", home)
	dir := filepath.Join(home, "drills")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "oops.json", `[{"id":"oops"}]`)
	_, _, err := Combined()
	if err == nil || !strings.Contains(err.Error(), "-builtin") {
		t.Fatalf("want an error suggesting -builtin, got %v", err)
	}
}

func TestBuiltinDrillsAreMarkedBuiltin(t *testing.T) {
	set, err := Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range set.All() {
		if d.Source != SourceBuiltin {
			t.Fatalf("drill %s source = %q", d.ID, d.Source)
		}
	}
}
