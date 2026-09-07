package synccli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/progress"
	"github.com/ganglinwu/lc-prac/internal/syncsrv"
)

const testToken = "0123456789abcdef0123"

// testServer runs the real sync server, so the client is checked against the
// thing it will actually talk to rather than a hand-written stub.
func testServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv, err := syncsrv.New(t.TempDir(), map[string]string{testToken: "ganglin"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func storeWith(t *testing.T, id string, at time.Time) *progress.Store {
	t.Helper()
	s := progress.New(filepath.Join(t.TempDir(), "progress.json"))
	s.RecordOutcome(id, progress.Solved, at)
	return s
}

func TestSyncCarriesHistoryBetweenMachines(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: testToken})
	now := time.Now().UTC().Truncate(time.Second)

	laptop := storeWith(t, "two-pointers-1", now)
	if _, d, err := Sync(c, laptop); err != nil || d.Changed() {
		t.Fatalf("first push: delta %+v err %v", d, err)
	}

	desktop := storeWith(t, "sliding-window-1", now.Add(time.Hour))
	merged, d, err := Sync(c, desktop)
	if err != nil {
		t.Fatal(err)
	}
	if d.NewDrills != 1 {
		t.Fatalf("want 1 new drill from the laptop, got %+v", d)
	}
	if merged.Len() != 2 {
		t.Fatalf("want both drills after sync, got %d", merged.Len())
	}
	if _, ok := merged.Get("two-pointers-1"); !ok {
		t.Fatal("the laptop's drill did not reach the desktop")
	}
}

func TestSyncIsIdempotent(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: testToken})
	local := storeWith(t, "graphs-1", time.Now().UTC())

	first, _, err := Sync(c, local)
	if err != nil {
		t.Fatal(err)
	}
	second, d, err := Sync(c, first)
	if err != nil {
		t.Fatal(err)
	}
	if d.Changed() {
		t.Fatalf("re-syncing an unchanged history should bring nothing, got %+v", d)
	}
	r, _ := second.Get("graphs-1")
	if r.Seen != 1 {
		t.Fatalf("counters drifted across syncs: seen %d, want 1", r.Seen)
	}
}

func TestSyncSurfacesServerError(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: strings.Repeat("z", 20)})
	_, _, err := Sync(c, progress.New(""))
	if err == nil {
		t.Fatal("want an error for a wrong token")
	}
	if !strings.Contains(err.Error(), "bad or missing token") {
		t.Fatalf("error should repeat what the server said, got %v", err)
	}
}

func TestPullFetchesWithoutPushing(t *testing.T) {
	ts := testServer(t)
	c := NewClient(Config{URL: ts.URL, Token: testToken})
	if _, _, err := Sync(c, storeWith(t, "heap-1", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	b, err := c.Pull()
	if err != nil {
		t.Fatal(err)
	}
	s, err := progress.Decode("", b)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get("heap-1"); !ok {
		t.Fatal("pull did not return the stored history")
	}
}

func TestClientTrimsTrailingSlash(t *testing.T) {
	c := NewClient(Config{URL: "https://host/", Token: testToken})
	if got := c.endpoint(); got != "https://host/v1/sync" {
		t.Fatalf("endpoint = %q", got)
	}
}

func TestValidateRejectsBadConfigs(t *testing.T) {
	cases := map[string]Config{
		"no url":         {Token: testToken},
		"no token":       {URL: "https://host"},
		"not a url":      {URL: "host.example", Token: testToken},
		"plain http":     {URL: "http://host.example", Token: testToken},
		"unknown scheme": {URL: "ftp://host.example", Token: testToken},
	}
	for name, c := range cases {
		if err := c.Validate(); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	if err := (Config{URL: "http://127.0.0.1:8090", Token: testToken}).Validate(); err != nil {
		t.Errorf("loopback http should be allowed for local testing: %v", err)
	}
}

func TestConfigRoundTripAndEnvOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sync.json")
	want := Config{URL: "https://lcprac.example", Token: testToken}
	if err := SaveConfig(path, want); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("config holds a token, want mode 0600, got %v", fi.Mode().Perm())
	}
	got, err := LoadConfig(path)
	if err != nil || got != want {
		t.Fatalf("round trip = %+v, %v", got, err)
	}

	t.Setenv("LCPRAC_SYNC_URL", "https://other.example")
	got, err = LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://other.example" || got.Token != testToken {
		t.Fatalf("env should override only the url it sets, got %+v", got)
	}
}

func TestLoadConfigMissingFileIsEmpty(t *testing.T) {
	c, err := LoadConfig(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatal(err)
	}
	if c != (Config{}) {
		t.Fatalf("want an empty config, got %+v", c)
	}
}

func TestDiffCountsUpdatesAndEvents(t *testing.T) {
	now := time.Now().UTC()
	local := progress.New("")
	local.RecordOutcome("a", progress.Solved, now)

	remote := progress.New("")
	remote.RecordOutcome("a", progress.Solved, now.Add(time.Hour))
	remote.RecordOutcome("b", progress.Missed, now)
	remote.AddSession(progress.Session{At: now, Minutes: 12, Attempted: 3, Correct: 2})
	remote.LogAttempt("56. Merge Intervals", progress.Result("solved"), "", now)

	d := DiffStores(local, local.Merge(remote))
	if d.NewDrills != 1 || d.UpdatedDrills != 1 || d.NewSessions != 1 || d.NewAttempts != 1 {
		t.Fatalf("delta = %+v", d)
	}
	if !d.Changed() {
		t.Fatal("a delta with entries should report Changed")
	}
}

func TestServerErrorFallsBackToRawBody(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "502 from the proxy", http.StatusBadGateway)
	}))
	defer ts.Close()
	c := NewClient(Config{URL: ts.URL, Token: testToken})
	_, err := c.Pull()
	if err == nil || !strings.Contains(err.Error(), "502 from the proxy") {
		t.Fatalf("want the proxy's body in the error, got %v", err)
	}
}

func TestPushSendsBearerToken(t *testing.T) {
	var got string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		json.NewEncoder(w).Encode(map[string]any{"version": 1})
	}))
	defer ts.Close()
	if _, err := NewClient(Config{URL: ts.URL, Token: testToken}).Push([]byte("{}")); err != nil {
		t.Fatal(err)
	}
	if got != "Bearer "+testToken {
		t.Fatalf("Authorization = %q", got)
	}
}
