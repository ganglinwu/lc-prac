// Package synccli is the machine's side of cross-machine sync: it knows where
// the server is, pushes this machine's history to it, and folds the canonical
// answer back into the local store.
package synccli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

// Config is where this machine syncs to. It lives next to the history rather
// than in the history, so pushing a store never leaks the token.
type Config struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	// Auto is nil when never set, which reads as on: configuring a server is
	// itself the opt-in, and an existing config file predates this field.
	Auto *bool `json:"auto,omitempty"`
}

// AutoEnabled reports whether a drill session should sync on its own.
func (c Config) AutoEnabled() bool {
	return c.Auto == nil || *c.Auto
}

// Ready reports whether this machine has somewhere to sync to, so a caller can
// stay quiet instead of reporting an error nobody asked for.
func (c Config) Ready() bool { return c.Validate() == nil }

// ConfigPath is sync.json beside progress.json, so one LCPRAC_HOME moves both.
func ConfigPath() (string, error) {
	p, err := progress.DefaultPath()
	if err != nil {
		return "", err
	}
	return filepath.Join(filepath.Dir(p), "sync.json"), nil
}

// LoadConfig reads the saved config, with LCPRAC_SYNC_URL and
// LCPRAC_SYNC_TOKEN overriding it so a one-off or a CI run needs no file.
func LoadConfig(path string) (Config, error) {
	var c Config
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("synccli: parsing %s: %w", path, err)
		}
	case !os.IsNotExist(err):
		return c, err
	}
	if v := os.Getenv("LCPRAC_SYNC_URL"); v != "" {
		c.URL = v
	}
	if v := os.Getenv("LCPRAC_SYNC_TOKEN"); v != "" {
		c.Token = v
	}
	if v := os.Getenv("LCPRAC_SYNC_AUTO"); v != "" {
		on, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("synccli: LCPRAC_SYNC_AUTO=%q is not a boolean", v)
		}
		c.Auto = &on
	}
	return c, nil
}

// SaveConfig writes the config readable only by you, since it holds a token.
func SaveConfig(path string, c Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o600)
}

// Validate rejects a config that would fail at the first request, so the error
// names the missing piece instead of arriving as a connection failure.
func (c Config) Validate() error {
	if c.URL == "" {
		return errors.New("no sync server set: `lcprac sync -set-url https://host -set-token TOKEN`")
	}
	u, err := url.Parse(c.URL)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return fmt.Errorf("sync url %q is not an http(s) url", c.URL)
	}
	if u.Scheme == "http" && !isLoopback(u.Hostname()) {
		return fmt.Errorf("sync url %q is plain http: the token would go over the wire in clear", c.URL)
	}
	if c.Token == "" {
		return errors.New("no sync token set: `lcprac sync -set-token TOKEN`")
	}
	return nil
}

// isLoopback allows http only where nothing leaves the machine, which is what
// makes local testing possible without weakening the real path.
func isLoopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Client talks to one sync server.
type Client struct {
	Config Config
	HTTP   *http.Client
}

// NewClient returns a client with a timeout, because a hung sync must not hold
// up practice.
func NewClient(c Config) *Client {
	return &Client{Config: c, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// AutoTimeout is the shorter budget an unattended sync gets: a session that
// waits half a minute on an unreachable server has already cost more than the
// sync was worth.
const AutoTimeout = 8 * time.Second

// NewAutoClient is the client for a sync nobody asked for, so it gives up
// sooner than one typed by hand.
func NewAutoClient(c Config) *Client {
	return &Client{Config: c, HTTP: &http.Client{Timeout: AutoTimeout}}
}

// The two synced documents: counters, and the drills you wrote yourself.
const (
	historyPath = "/v1/sync"
	drillsPath  = "/v1/drills"
)

// endpoint is one of the sync routes on the configured server.
func (c *Client) endpoint(path string) string {
	return strings.TrimRight(c.Config.URL, "/") + path
}

// Push sends this machine's history and returns the server's merge of it,
// which is the whole protocol: one round trip converges both sides.
func (c *Client) Push(body []byte) ([]byte, error) {
	return c.do(http.MethodPost, historyPath, bytes.NewReader(body))
}

// Pull fetches the server's copy without sending anything, for a fresh machine
// that has nothing worth pushing yet.
func (c *Client) Pull() ([]byte, error) {
	return c.do(http.MethodGet, historyPath, nil)
}

// PushDrills sends the drills this machine holds and returns the merge, the
// same one-round-trip shape as Push.
func (c *Client) PushDrills(body []byte) ([]byte, error) {
	return c.do(http.MethodPost, drillsPath, bytes.NewReader(body))
}

// PullDrills fetches the server's drills without sending any.
func (c *Client) PullDrills() ([]byte, error) {
	return c.do(http.MethodGet, drillsPath, nil)
}

func (c *Client) do(method, path string, body io.Reader) ([]byte, error) {
	if err := c.Config.Validate(); err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, c.endpoint(path), body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Config.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sync: reaching %s: %w", c.Config.URL, err)
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sync: server said %s: %s", resp.Status, serverError(out))
	}
	return out, nil
}

// serverError pulls the message out of the endpoint's JSON error shape, and
// falls back to the raw body when something else answered (a proxy, say).
func serverError(b []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &e) == nil && e.Error != "" {
		return e.Error
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "no detail"
	}
	if len(s) > 200 {
		s = s[:200] + "..."
	}
	return s
}

// Delta is what a sync brought back, so the command can say what changed
// rather than only that it worked.
type Delta struct {
	NewDrills     int
	UpdatedDrills int
	NewSessions   int
	NewAttempts   int
	// NewWritten and UpdatedWritten count the drills you wrote yourself, which
	// travel as text rather than as counters. DeletedWritten counts the ones
	// you deleted on another machine, which travel as tombstones.
	NewWritten     int
	UpdatedWritten int
	DeletedWritten int
}

// Changed reports whether the merge actually moved anything.
func (d Delta) Changed() bool {
	return d.NewDrills+d.UpdatedDrills+d.NewSessions+d.NewAttempts+
		d.NewWritten+d.UpdatedWritten+d.DeletedWritten > 0
}

// DiffStores describes what merged holds that local did not.
func DiffStores(local, merged *progress.Store) Delta {
	var d Delta
	before := map[string]progress.Record{}
	for _, r := range local.Records() {
		before[r.DrillID] = r
	}
	for _, r := range merged.Records() {
		old, ok := before[r.DrillID]
		switch {
		case !ok:
			d.NewDrills++
		case !sameRecord(old, r):
			d.UpdatedDrills++
		}
	}
	if n := len(merged.Sessions()) - len(local.Sessions()); n > 0 {
		d.NewSessions = n
	}
	if n := len(merged.Attempts()) - len(local.Attempts()); n > 0 {
		d.NewAttempts = n
	}
	return d
}

// sameRecord compares the fields a merge can move. Comparing the structs
// directly would not work: Solution is a pointer, so two equal solutions from
// different sides would read as a change.
func sameRecord(a, b progress.Record) bool {
	if a.Seen != b.Seen || a.Correct != b.Correct || a.Assisted != b.Assisted ||
		a.Streak != b.Streak || a.Seconds != b.Seconds || a.Note != b.Note ||
		!a.LastSeen.Equal(b.LastSeen) || !a.DueAt.Equal(b.DueAt) {
		return false
	}
	switch {
	case a.Solution == nil && b.Solution == nil:
		return true
	case a.Solution == nil || b.Solution == nil:
		return false
	default:
		return a.Solution.Source == b.Solution.Source && a.Solution.Passed == b.Solution.Passed
	}
}

// Sync pushes the local history, merges the server's answer back into it and
// returns the merged store plus what it brought. The caller decides whether to
// save, which is what makes a dry run possible.
func Sync(c *Client, local *progress.Store) (*progress.Store, Delta, error) {
	body, err := local.Encode()
	if err != nil {
		return nil, Delta{}, err
	}
	out, err := c.Push(body)
	if err != nil {
		return nil, Delta{}, err
	}
	remote, err := progress.Decode("", out)
	if err != nil {
		return nil, Delta{}, fmt.Errorf("sync: server returned unreadable history: %w", err)
	}
	merged := local.Merge(remote)
	return merged, DiffStores(local, merged), nil
}

// SyncDrills carries the drills you wrote yourself to the server and back, so
// a drill written on the laptop can be practised on the desktop. Nothing is
// written to dir unless apply is set, which is what makes a dry run possible.
func SyncDrills(c *Client, dir string, apply bool) (newer, updated, deleted int, err error) {
	// Tombstones are loaded too: a push that quietly dropped them would ask
	// the server to hand the deleted drill straight back.
	local, err := drill.LoadUserDirAll(dir)
	if err != nil {
		return 0, 0, 0, err
	}
	body, err := drill.EncodeDrills(local)
	if err != nil {
		return 0, 0, 0, err
	}
	out, err := c.PushDrills(body)
	if err != nil {
		return 0, 0, 0, err
	}
	remote, err := drill.DecodeDrills(out)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("sync: server returned unreadable drills: %w", err)
	}
	merged := drill.MergeDrills(local, remote)
	if !apply {
		n, u, d := DiffDrills(local, merged)
		return n, u, d, nil
	}
	return drill.ApplyMerged(dir, merged)
}

// DiffDrills counts what merged holds that this machine's drills did not. A
// tombstone for a drill this machine never had counts as nothing, matching
// what ApplyMerged would write.
func DiffDrills(local, merged []drill.Drill) (newer, updated, deleted int) {
	have := make(map[string]drill.Drill, len(local))
	for _, d := range local {
		have[d.ID] = d
	}
	for _, m := range merged {
		old, ok := have[m.ID]
		switch {
		case !ok:
			if !m.Deleted() {
				newer++
			}
		case drill.SameDrill(old, m):
		case m.Deleted() && !old.Deleted():
			deleted++
		default:
			updated++
		}
	}
	return newer, updated, deleted
}
