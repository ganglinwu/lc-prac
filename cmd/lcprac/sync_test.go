package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
	"github.com/ganglinwu/lc-prac/internal/synccli"
	"github.com/ganglinwu/lc-prac/internal/syncsrv"
)

const cliToken = "0123456789abcdef0123"

// syncHome points the CLI at a throwaway history and returns a live server for
// it to talk to, so a sync can be exercised end to end.
func syncHome(t *testing.T) (home string, url string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("LCPRAC_HOME", home)
	t.Setenv("LCPRAC_SYNC_URL", "")
	t.Setenv("LCPRAC_SYNC_TOKEN", "")
	t.Setenv("LCPRAC_SYNC_AUTO", "")
	srv, err := syncsrv.New(t.TempDir(), map[string]string{cliToken: "ganglin"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return home, ts.URL
}

func TestSyncConfigIsSavedBesideHistory(t *testing.T) {
	home, url := syncHome(t)
	if err := cmdSync([]string{"-set-url", url, "-set-token", cliToken}); err != nil {
		t.Fatal(err)
	}
	path, err := synccli.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(home, "sync.json"); path != want {
		t.Fatalf("config path = %q, want %q", path, want)
	}
	cfg, err := synccli.LoadConfig(path)
	if err != nil || cfg.URL != url || cfg.Token != cliToken {
		t.Fatalf("saved config = %+v, %v", cfg, err)
	}
}

// Setting one field must not wipe the other, or rotating a token would silently
// unset the server.
func TestSaveSyncConfigKeepsTheFieldYouDidNotSet(t *testing.T) {
	_, url := syncHome(t)
	path := filepath.Join(t.TempDir(), "sync.json")
	out := &bytes.Buffer{}
	if err := saveSyncConfig(out, path, synccli.Config{}, url, cliToken, ""); err != nil {
		t.Fatal(err)
	}
	cfg, _ := synccli.LoadConfig(path)
	if err := saveSyncConfig(out, path, cfg, "", "aaaaaaaaaaaaaaaaaaaa", ""); err != nil {
		t.Fatal(err)
	}
	cfg, _ = synccli.LoadConfig(path)
	if cfg.URL != url || cfg.Token != "aaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestDoSyncPullsAnotherMachinesHistory(t *testing.T) {
	home, url := syncHome(t)
	cfg := synccli.Config{URL: url, Token: cliToken}

	// Another machine got there first.
	other := progress.New("")
	other.RecordOutcome("graphs-bfs", progress.Solved, time.Now().UTC())
	if _, _, err := synccli.Sync(synccli.NewClient(cfg), other); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	if err := doSync(out, cfg, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1 drill(s) this machine had never seen") {
		t.Fatalf("output should name what arrived, got %q", out.String())
	}
	store, err := progress.Load(filepath.Join(home, "progress.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("graphs-bfs"); !ok {
		t.Fatal("the pulled drill was not written to local history")
	}
}

func TestDoSyncDryRunWritesNothing(t *testing.T) {
	home, url := syncHome(t)
	cfg := synccli.Config{URL: url, Token: cliToken}
	other := progress.New("")
	other.RecordOutcome("graphs-bfs", progress.Solved, time.Now().UTC())
	if _, _, err := synccli.Sync(synccli.NewClient(cfg), other); err != nil {
		t.Fatal(err)
	}

	out := &bytes.Buffer{}
	if err := doSync(out, cfg, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing was written") {
		t.Fatalf("dry run should say so, got %q", out.String())
	}
	store, err := progress.Load(filepath.Join(home, "progress.json"))
	if err != nil {
		t.Fatal(err)
	}
	if store.Len() != 0 {
		t.Fatalf("dry run wrote %d drills to disk", store.Len())
	}
}

func TestDoSyncSaysWhenAlreadyInSync(t *testing.T) {
	_, url := syncHome(t)
	cfg := synccli.Config{URL: url, Token: cliToken}
	out := &bytes.Buffer{}
	if err := doSync(out, cfg, false); err != nil {
		t.Fatal(err)
	}
	if err := doSync(out, cfg, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing new to bring back") {
		t.Fatalf("got %q", out.String())
	}
}

func TestDoSyncWithoutConfigExplainsHow(t *testing.T) {
	syncHome(t)
	err := doSync(&bytes.Buffer{}, synccli.Config{}, false)
	if err == nil || !strings.Contains(err.Error(), "-set-url") {
		t.Fatalf("want setup instructions, got %v", err)
	}
}

func TestShowSyncHidesTheToken(t *testing.T) {
	syncHome(t)
	out := &bytes.Buffer{}
	if err := showSync(out, "/tmp/sync.json", synccli.Config{URL: "https://host", Token: cliToken}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), cliToken) {
		t.Fatalf("status leaked the token: %q", out.String())
	}
	if !strings.Contains(out.String(), "https://host") {
		t.Fatalf("status should name the server, got %q", out.String())
	}
}

// configure writes a sync config for the throwaway home, the way -set-url does.
func configure(t *testing.T, url, auto string) {
	t.Helper()
	path, err := synccli.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := saveSyncConfig(&bytes.Buffer{}, path, synccli.Config{}, url, cliToken, auto); err != nil {
		t.Fatal(err)
	}
}

// seedServer puts one drill on the server, standing in for the other machine.
func seedServer(t *testing.T, url, id string) {
	t.Helper()
	other := progress.New("")
	other.RecordOutcome(id, progress.Solved, time.Now().UTC())
	if _, _, err := synccli.Sync(synccli.NewClient(synccli.Config{URL: url, Token: cliToken}), other); err != nil {
		t.Fatal(err)
	}
}

func TestAutoSyncPullsBeforeASession(t *testing.T) {
	home, url := syncHome(t)
	configure(t, url, "")
	seedServer(t, url, "graphs-bfs")

	out := &bytes.Buffer{}
	autoSync(out, false)

	store, err := progress.Load(filepath.Join(home, "progress.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("graphs-bfs"); !ok {
		t.Fatal("auto sync did not write the other machine's drill to local history")
	}
	if !strings.Contains(out.String(), "1 drill(s) this machine had never seen") {
		t.Fatalf("auto sync should say what arrived, got %q", out.String())
	}
}

// The push half matters as much as the pull: what this machine just practised
// has to be on the server before the next machine starts.
func TestAutoSyncPushesWhatThisMachineDid(t *testing.T) {
	home, url := syncHome(t)
	configure(t, url, "")
	local := progress.New(filepath.Join(home, "progress.json"))
	local.RecordOutcome("arrays-two-pointer", progress.Solved, time.Now().UTC())
	if err := local.Save(); err != nil {
		t.Fatal(err)
	}

	autoSync(&bytes.Buffer{}, true)

	fresh := progress.New("")
	merged, _, err := synccli.Sync(synccli.NewClient(synccli.Config{URL: url, Token: cliToken}), fresh)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := merged.Get("arrays-two-pointer"); !ok {
		t.Fatal("auto sync did not push this machine's history")
	}
}

// quiet is for the end of a session, where the pull has already been reported
// and the sitting's own summary should be the last thing on screen.
func TestAutoSyncQuietPrintsNothing(t *testing.T) {
	_, url := syncHome(t)
	configure(t, url, "")
	seedServer(t, url, "graphs-bfs")

	out := &bytes.Buffer{}
	autoSync(out, true)
	if out.String() != "" {
		t.Fatalf("quiet auto sync printed %q", out.String())
	}
}

func TestAutoSyncDoesNothingWithoutAServer(t *testing.T) {
	home, _ := syncHome(t)
	out := &bytes.Buffer{}
	autoSync(out, false)
	if out.String() != "" {
		t.Fatalf("an unconfigured machine should say nothing, got %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(home, "progress.json")); !os.IsNotExist(err) {
		t.Fatalf("auto sync touched history on an unconfigured machine: %v", err)
	}
}

func TestAutoSyncOffNeverReachesTheServer(t *testing.T) {
	home, url := syncHome(t)
	configure(t, url, "off")
	seedServer(t, url, "graphs-bfs")

	autoSync(&bytes.Buffer{}, false)

	if _, err := os.Stat(filepath.Join(home, "progress.json")); !os.IsNotExist(err) {
		t.Fatalf("auto sync ran with -auto off: %v", err)
	}
}

// A dead server must cost a warning, not a session.
func TestAutoSyncSurvivesAnUnreachableServer(t *testing.T) {
	home, _ := syncHome(t)
	dead := httptest.NewServer(nil)
	dead.Close()
	configure(t, dead.URL, "")
	local := progress.New(filepath.Join(home, "progress.json"))
	local.RecordOutcome("arrays-two-pointer", progress.Solved, time.Now().UTC())
	if err := local.Save(); err != nil {
		t.Fatal(err)
	}

	autoSync(&bytes.Buffer{}, false)

	store, err := progress.Load(filepath.Join(home, "progress.json"))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.Get("arrays-two-pointer"); !ok {
		t.Fatal("a failed sync damaged local history")
	}
}

func TestSyncAutoFlagRoundTrips(t *testing.T) {
	_, url := syncHome(t)
	configure(t, url, "")
	path, err := synccli.ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := synccli.LoadConfig(path)
	if !cfg.AutoEnabled() {
		t.Fatal("a config that never mentions auto should sync on its own")
	}
	if err := cmdSync([]string{"-auto", "off"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = synccli.LoadConfig(path)
	if cfg.AutoEnabled() || cfg.URL != url || cfg.Token != cliToken {
		t.Fatalf("-auto off changed more than auto: %+v", cfg)
	}
	if err := cmdSync([]string{"-auto", "on"}); err != nil {
		t.Fatal(err)
	}
	cfg, _ = synccli.LoadConfig(path)
	if !cfg.AutoEnabled() {
		t.Fatal("-auto on did not turn it back on")
	}
}

func TestSyncAutoFlagRejectsNonsense(t *testing.T) {
	_, url := syncHome(t)
	configure(t, url, "")
	err := cmdSync([]string{"-auto", "sometimes"})
	if err == nil || !strings.Contains(err.Error(), "on or off") {
		t.Fatalf("want a readable error, got %v", err)
	}
}

func TestStatusReportsAutoState(t *testing.T) {
	syncHome(t)
	off := false
	out := &bytes.Buffer{}
	if err := showSync(out, "/tmp/sync.json", synccli.Config{URL: "https://host", Token: cliToken, Auto: &off}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "off around drill sessions") {
		t.Fatalf("status should report auto is off, got %q", out.String())
	}
}

// writeOwnDrill puts a drill in this machine's drills directory, standing in
// for `lcprac add`.
func writeOwnDrill(t *testing.T, id, prompt string, stamp time.Time) {
	t.Helper()
	dir, err := drill.UserDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	d := drill.Drill{
		ID: id, Title: "t " + id, Kind: drill.KindRecall, Topic: "arrays",
		Difficulty: drill.Easy, EstMinutes: 2, Prompt: prompt,
		Answer: "a", Explanation: "e", UpdatedAt: &stamp,
	}
	b, err := json.MarshalIndent([]drill.Drill{d}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mine.json"), append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A drill written on one machine has to be practisable on the other, not just
// present in its progress history.
func TestSyncCarriesYourOwnDrillsToAnotherMachine(t *testing.T) {
	_, url := syncHome(t)
	stamp := time.Now().UTC().Truncate(time.Second)
	writeOwnDrill(t, "my-own-drill", "why is a deque needed here", stamp)
	if err := cmdSync([]string{"-set-url", url, "-set-token", cliToken}); err != nil {
		t.Fatal(err)
	}
	if err := cmdSync(nil); err != nil {
		t.Fatal(err)
	}

	// A second machine: same server, empty home.
	second := t.TempDir()
	t.Setenv("LCPRAC_HOME", second)
	if err := cmdSync([]string{"-set-url", url, "-set-token", cliToken}); err != nil {
		t.Fatal(err)
	}
	out := &bytes.Buffer{}
	cfg, err := synccli.LoadConfig(filepath.Join(second, "sync.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := doSync(out, cfg, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "1 drill(s) you wrote on another machine") {
		t.Fatalf("output did not report the pulled drill:\n%s", out)
	}
	deck, _, err := drill.Combined()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := deck.ByID("my-own-drill")
	if !ok {
		t.Fatal("the pulled drill is not in the second machine's deck")
	}
	if d.Prompt != "why is a deque needed here" {
		t.Fatalf("pulled drill prompt = %q", d.Prompt)
	}
}

// The dry run must report what would arrive without putting it on disk.
func TestSyncDryRunDoesNotWriteOwnDrills(t *testing.T) {
	_, url := syncHome(t)
	writeOwnDrill(t, "shared-drill", "p", time.Now().UTC().Truncate(time.Second))
	if err := cmdSync([]string{"-set-url", url, "-set-token", cliToken}); err != nil {
		t.Fatal(err)
	}
	if err := cmdSync(nil); err != nil {
		t.Fatal(err)
	}

	second := t.TempDir()
	t.Setenv("LCPRAC_HOME", second)
	cfg := synccli.Config{URL: url, Token: cliToken}
	out := &bytes.Buffer{}
	if err := doSync(out, cfg, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "nothing was written") {
		t.Fatalf("dry run output:\n%s", out)
	}
	dir, err := drill.UserDir()
	if err != nil {
		t.Fatal(err)
	}
	if ds, err := drill.LoadUserDir(dir); err != nil || len(ds) != 0 {
		t.Fatalf("dry run left %d drills on disk (%v)", len(ds), err)
	}
}
