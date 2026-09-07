package syncsrv

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/progress"
)

const testToken = "0123456789abcdef0123"

var epoch = time.Date(2026, 1, 2, 9, 0, 0, 0, time.UTC)

func newTest(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(dir, map[string]string{testToken: "ganglin"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)
	return ts, dir
}

// push is what the client does: send a whole history, get the merge back.
func push(t *testing.T, ts *httptest.Server, token string, s *progress.Store) (*http.Response, []byte) {
	t.Helper()
	var body []byte
	if s != nil {
		b, err := s.Encode()
		if err != nil {
			t.Fatalf("Encode: %v", err)
		}
		body = b
	}
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/sync", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return resp, out
}

func decode(t *testing.T, b []byte) *progress.Store {
	t.Helper()
	s, err := progress.Decode("", b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return s
}

func TestNewRejectsBadConfig(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct {
		dir    string
		tokens map[string]string
	}{
		"no dir":        {"", map[string]string{testToken: "ganglin"}},
		"no tokens":     {dir, map[string]string{}},
		"short token":   {dir, map[string]string{"short": "ganglin"}},
		"bad account":   {dir, map[string]string{testToken: "../etc/passwd"}},
		"empty account": {dir, map[string]string{testToken: ""}},
	}
	for name, c := range cases {
		if _, err := New(c.dir, c.tokens); err == nil {
			t.Errorf("%s: want error, got nil", name)
		}
	}
}

func TestPushStoresHistory(t *testing.T) {
	ts, dir := newTest(t)
	local := progress.New("")
	local.Record("two-pointers", true, epoch)
	local.SetNote("two-pointers", "left/right, not fast/slow")

	resp, body := push(t, ts, testToken, local)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, body %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q", ct)
	}
	got := decode(t, body)
	if r, ok := got.Get("two-pointers"); !ok || r.Seen != 1 {
		t.Fatalf("response missing the pushed record: %+v", r)
	}
	if _, err := os.Stat(filepath.Join(dir, "ganglin.json")); err != nil {
		t.Fatalf("history not persisted: %v", err)
	}
}

func TestPushMergesTwoMachines(t *testing.T) {
	ts, _ := newTest(t)

	laptop := progress.New("")
	laptop.Record("sliding-window", true, epoch)
	laptop.AddSession(progress.Session{At: epoch, Minutes: 12, Attempted: 3, Correct: 3})
	if _, body := push(t, ts, testToken, laptop); len(body) == 0 {
		t.Fatal("empty response from laptop push")
	}

	desktop := progress.New("")
	desktop.Record("binary-search", false, epoch.Add(24*time.Hour))
	desktop.AddSession(progress.Session{At: epoch.Add(24 * time.Hour), Minutes: 10, Attempted: 2, Correct: 1})
	resp, body := push(t, ts, testToken, desktop)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	got := decode(t, body)
	if _, ok := got.Get("sliding-window"); !ok {
		t.Error("laptop's drill missing after desktop push")
	}
	if _, ok := got.Get("binary-search"); !ok {
		t.Error("desktop's drill missing after desktop push")
	}
	if n := len(got.Sessions()); n != 2 {
		t.Errorf("sessions = %d, want 2", n)
	}
}

// A re-push of an unchanged history must not inflate counters, which is the
// case that makes summing counters unusable.
func TestRepeatedPushIsIdempotent(t *testing.T) {
	ts, _ := newTest(t)
	local := progress.New("")
	local.Record("heap", true, epoch)
	local.Record("heap", true, epoch.Add(time.Minute))

	var last []byte
	for i := 0; i < 3; i++ {
		_, last = push(t, ts, testToken, local)
	}
	got := decode(t, last)
	r, ok := got.Get("heap")
	if !ok || r.Seen != 2 || r.Correct != 2 {
		t.Fatalf("counters drifted after 3 pushes: %+v", r)
	}
}

func TestGetReturnsStoredHistory(t *testing.T) {
	ts, _ := newTest(t)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/sync", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("empty-server GET status = %d, want 200", resp.StatusCode)
	}

	local := progress.New("")
	local.Record("graph", true, epoch)
	push(t, ts, testToken, local)

	req, _ = http.NewRequest(http.MethodGet, ts.URL+"/v1/sync", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err = ts.Client().Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var f map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&f); err != nil {
		t.Fatalf("decoding GET body: %v", err)
	}
	if _, ok := f["records"]; !ok {
		t.Fatalf("GET body has no records: %v", f)
	}
}

func TestAuth(t *testing.T) {
	ts, _ := newTest(t)
	local := progress.New("")

	for _, tok := range []string{"", "wrong-token-that-is-long"} {
		resp, _ := push(t, ts, tok, local)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Errorf("token %q: status = %d, want 401", tok, resp.StatusCode)
		}
	}
	resp, _ := push(t, ts, testToken, local)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("good token: status = %d, want 200", resp.StatusCode)
	}
}

// Two accounts on one box must not see each other's drills.
func TestAccountsAreIsolated(t *testing.T) {
	dir := t.TempDir()
	const other = "fedcba9876543210fedc"
	s, err := New(dir, map[string]string{testToken: "ganglin", other: "guest"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	mine := progress.New("")
	mine.Record("trie", true, epoch)
	push(t, ts, testToken, mine)

	_, body := push(t, ts, other, progress.New(""))
	if _, ok := decode(t, body).Get("trie"); ok {
		t.Fatal("guest can see ganglin's history")
	}
}

func TestBadBodyIsRejected(t *testing.T) {
	ts, _ := newTest(t)
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/sync", strings.NewReader("not json"))
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	ts, _ := newTest(t)
	req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/v1/sync", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}

func TestHealthz(t *testing.T) {
	ts, _ := newTest(t)
	resp, err := ts.Client().Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}
}
