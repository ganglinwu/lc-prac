package main

import (
	"bytes"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	if err := saveSyncConfig(out, path, synccli.Config{}, url, cliToken); err != nil {
		t.Fatal(err)
	}
	cfg, _ := synccli.LoadConfig(path)
	if err := saveSyncConfig(out, path, cfg, "", "aaaaaaaaaaaaaaaaaaaa"); err != nil {
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
	if !strings.Contains(out.String(), "already in sync") {
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
