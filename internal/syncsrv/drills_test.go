package syncsrv

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
)

func testDrill(id, prompt string, stamp time.Time) drill.Drill {
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

// pushDrills sends a drill set and returns the server's merge of it.
func pushDrills(t *testing.T, ts *httptest.Server, token string, ds []drill.Drill) (*http.Response, []byte) {
	t.Helper()
	body, err := drill.EncodeDrills(ds)
	if err != nil {
		t.Fatalf("EncodeDrills: %v", err)
	}
	return sendDrills(t, ts, http.MethodPost, token, body)
}

func sendDrills(t *testing.T, ts *httptest.Server, method, token string, body []byte) (*http.Response, []byte) {
	t.Helper()
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, ts.URL+"/v1/drills", r)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading body: %v", err)
	}
	return resp, out
}

func decodeDrills(t *testing.T, b []byte) []drill.Drill {
	t.Helper()
	ds, err := drill.DecodeDrills(b)
	if err != nil {
		t.Fatalf("DecodeDrills(%s): %v", b, err)
	}
	return ds
}

func TestDrillsTwoMachinesConverge(t *testing.T) {
	ts, _ := newTest(t)
	laptop := []drill.Drill{testDrill("laptop-one", "written on the laptop", epoch)}
	desktop := []drill.Drill{testDrill("desk-one", "written on the desktop", epoch)}

	if resp, _ := pushDrills(t, ts, testToken, laptop); resp.StatusCode != http.StatusOK {
		t.Fatalf("laptop push: %s", resp.Status)
	}
	resp, out := pushDrills(t, ts, testToken, desktop)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("desktop push: %s", resp.Status)
	}
	got := decodeDrills(t, out)
	if len(got) != 2 || got[0].ID != "desk-one" || got[1].ID != "laptop-one" {
		t.Fatalf("merge = %+v, want both drills sorted by id", got)
	}
}

func TestDrillsNewestEditWinsAcrossMachines(t *testing.T) {
	ts, _ := newTest(t)
	pushDrills(t, ts, testToken, []drill.Drill{testDrill("a", "first", epoch)})
	_, out := pushDrills(t, ts, testToken, []drill.Drill{testDrill("a", "second", epoch.Add(time.Hour))})
	got := decodeDrills(t, out)
	if len(got) != 1 || got[0].Prompt != "second" {
		t.Fatalf("merge = %+v, want the later edit", got)
	}
	// The older machine re-pushing its stale copy must not undo the edit.
	_, out = pushDrills(t, ts, testToken, []drill.Drill{testDrill("a", "first", epoch)})
	if got := decodeDrills(t, out); got[0].Prompt != "second" {
		t.Fatalf("a stale push reverted the drill to %q", got[0].Prompt)
	}
}

func TestDrillsPushIsIdempotent(t *testing.T) {
	ts, _ := newTest(t)
	ds := []drill.Drill{testDrill("a", "p", epoch), testDrill("b", "q", epoch)}
	var last []byte
	for i := 0; i < 3; i++ {
		_, out := pushDrills(t, ts, testToken, ds)
		if i > 0 && string(out) != string(last) {
			t.Fatalf("push %d returned a different merge", i+1)
		}
		last = out
	}
	if n := len(decodeDrills(t, last)); n != 2 {
		t.Fatalf("after three identical pushes the server holds %d drills, want 2", n)
	}
}

func TestDrillsGetOnFreshAccount(t *testing.T) {
	ts, _ := newTest(t)
	resp, out := sendDrills(t, ts, http.MethodGet, testToken, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET: %s", resp.Status)
	}
	if ds := decodeDrills(t, out); len(ds) != 0 {
		t.Fatalf("fresh account has %d drills, want none", len(ds))
	}
}

func TestDrillsRejectInvalidUpload(t *testing.T) {
	ts, _ := newTest(t)
	// A drill missing its answer must not reach another machine's deck.
	bad := []byte(`[{"id":"x","title":"t","kind":"recall","topic":"arrays","difficulty":"easy","est_minutes":2,"prompt":"p"}]`)
	resp, out := sendDrills(t, ts, http.MethodPost, testToken, bad)
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %s, want 400 (body %s)", resp.Status, out)
	}
}

func TestDrillsRequireToken(t *testing.T) {
	ts, _ := newTest(t)
	if resp, _ := sendDrills(t, ts, http.MethodGet, "", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token: %s", resp.Status)
	}
	if resp, _ := sendDrills(t, ts, http.MethodPost, "wrong-token-0123456789", nil); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong token: %s", resp.Status)
	}
}

func TestDrillsStoredSeparatelyFromHistory(t *testing.T) {
	ts, dir := newTest(t)
	pushDrills(t, ts, testToken, []drill.Drill{testDrill("a", "p", epoch)})
	if _, err := os.Stat(filepath.Join(dir, "ganglin.drills.json")); err != nil {
		t.Fatalf("drills file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ganglin.json")); !os.IsNotExist(err) {
		t.Fatal("pushing drills should not create a history file")
	}
}

func TestDrillsPerAccountIsolation(t *testing.T) {
	dir := t.TempDir()
	const other = "fedcba9876543210fedc"
	s, err := New(dir, map[string]string{testToken: "ganglin", other: "someone"})
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(s.Handler())
	defer ts.Close()

	pushDrills(t, ts, testToken, []drill.Drill{testDrill("mine", "p", epoch)})
	_, out := pushDrills(t, ts, other, []drill.Drill{testDrill("theirs", "q", epoch)})
	got := decodeDrills(t, out)
	if len(got) != 1 || got[0].ID != "theirs" {
		t.Fatalf("second account sees %+v, want only its own drill", got)
	}
}

func TestDrillsMethodNotAllowed(t *testing.T) {
	ts, _ := newTest(t)
	resp, _ := sendDrills(t, ts, http.MethodDelete, testToken, nil)
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %s, want 405", resp.Status)
	}
}
