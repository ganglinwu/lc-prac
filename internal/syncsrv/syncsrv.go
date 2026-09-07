// Package syncsrv serves one machine's practice history to another. It holds
// the canonical copy of a progress store and folds each client's history into
// it, so two machines that both push end up agreeing.
package syncsrv

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

// maxBody caps an upload. A full history is a few hundred KB of JSON even
// after years of drills, so 8 MB is slack rather than a real limit.
const maxBody = 8 << 20

// Server is the sync endpoint. It owns the file at Path and serialises every
// read-merge-write behind one mutex, which is enough because the whole history
// is a single small document.
type Server struct {
	path   string
	tokens map[string]string // token -> account name
	mu     sync.Mutex
}

// New returns a server storing histories under dir. Each entry in tokens maps
// a bearer token to an account name, and the account names the file, so a
// second person (or a second identity) can share the box without sharing data.
func New(dir string, tokens map[string]string) (*Server, error) {
	if dir == "" {
		return nil, errors.New("syncsrv: data directory is required")
	}
	if len(tokens) == 0 {
		return nil, errors.New("syncsrv: at least one token is required")
	}
	for tok, acct := range tokens {
		if len(tok) < 16 {
			return nil, fmt.Errorf("syncsrv: token for %q is too short (want 16+ chars)", acct)
		}
		if !validAccount(acct) {
			return nil, fmt.Errorf("syncsrv: bad account name %q", acct)
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Server{path: dir, tokens: tokens}, nil
}

// validAccount keeps account names to characters that cannot escape the data
// directory once they are used as a filename.
func validAccount(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// Handler returns the routes: a health check and the sync endpoint.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/v1/sync", s.handleSync)
	mux.HandleFunc("/v1/drills", s.handleDrills)
	return mux
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	s.serve(w, r, historyDoc)
}

// handleDrills carries the drills you wrote yourself. It is a separate
// document from the history because progress is a fold of counters while a
// drill is a whole edited text, so the two need different merge rules.
func (s *Server) handleDrills(w http.ResponseWriter, r *http.Request) {
	s.serve(w, r, drillsDoc)
}

// doc is one synced document: where it is stored and how two copies of it are
// combined. Everything else about a sync is the same for both.
type doc struct {
	suffix string
	// empty is what an account that has never pushed gets back, and it has to
	// parse as this document's own shape: a history is an object, a drill set
	// is an array.
	empty []byte
	merge func(stored, client []byte) ([]byte, error)
}

var historyDoc = doc{suffix: ".json", empty: []byte("{}\n"), merge: mergeHistory}

var drillsDoc = doc{suffix: ".drills.json", empty: []byte("[]\n"), merge: mergeDrills}

// mergeHistory folds a machine's counters into the stored ones. The path is
// empty because the caller writes the bytes, so the store cannot save itself
// over the wrong file.
func mergeHistory(stored, client []byte) ([]byte, error) {
	theirs, err := progress.Decode("", client)
	if err != nil {
		return nil, badRequest{"history is not valid lcprac JSON"}
	}
	ours, err := progress.Decode("", stored)
	if err != nil {
		return nil, fmt.Errorf("stored history is corrupt: %w", err)
	}
	return ours.Merge(theirs).Encode()
}

// mergeDrills unions the two drill sets, newest edit per id winning.
func mergeDrills(stored, client []byte) ([]byte, error) {
	theirs, err := drill.DecodeDrills(client)
	if err != nil {
		return nil, badRequest{"drills are not valid lcprac drills: " + err.Error()}
	}
	ours, err := drill.DecodeDrills(stored)
	if err != nil {
		return nil, fmt.Errorf("stored drills are corrupt: %w", err)
	}
	return drill.EncodeDrills(drill.MergeDrills(ours, theirs))
}

// badRequest marks a merge failure the client caused, so it comes back as a
// 400 naming the problem rather than as a 500.
type badRequest struct{ msg string }

func (e badRequest) Error() string { return e.msg }

func (s *Server) serve(w http.ResponseWriter, r *http.Request, d doc) {
	acct, ok := s.authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="lcprac"`)
		httpError(w, http.StatusUnauthorized, "bad or missing token")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGet(w, acct, d)
	case http.MethodPost:
		s.handlePost(w, r, acct, d)
	default:
		w.Header().Set("Allow", "GET, POST")
		httpError(w, http.StatusMethodNotAllowed, "use GET or POST")
	}
}

// authenticate compares the bearer token in constant time so a wrong token
// cannot be found one byte at a time.
func (s *Server) authenticate(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		return "", false
	}
	got := []byte(strings.TrimSpace(strings.TrimPrefix(h, "Bearer ")))
	match := ""
	for tok, acct := range s.tokens {
		if subtle.ConstantTimeCompare(got, []byte(tok)) == 1 {
			match = acct
		}
	}
	return match, match != ""
}

func (s *Server) handleGet(w http.ResponseWriter, acct string, d doc) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.read(acct, d)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "reading stored copy")
		return
	}
	writeJSON(w, b, d.empty)
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request, acct string, d doc) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		httpError(w, http.StatusRequestEntityTooLarge, "upload too large")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := s.read(acct, d)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "reading stored copy")
		return
	}
	merged, err := d.merge(stored, body)
	if err != nil {
		var bad badRequest
		if errors.As(err, &bad) {
			httpError(w, http.StatusBadRequest, bad.msg)
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.write(acct, d, merged); err != nil {
		httpError(w, http.StatusInternalServerError, "writing stored copy")
		return
	}
	writeJSON(w, merged, d.empty)
}

// write replaces an account's document through a temp file, so a crash
// mid-write cannot leave a truncated copy as the canonical one.
func (s *Server) write(acct string, d doc, b []byte) error {
	path := s.file(acct, d)
	tmp, err := os.CreateTemp(filepath.Dir(path), ".lcpracd-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// file is where an account's history lives. Account names are validated in
// New, so this cannot point outside the data directory.
func (s *Server) file(acct string, d doc) string {
	return filepath.Join(s.path, acct+d.suffix)
}

// read returns the stored bytes, treating a missing file as no history yet.
func (s *Server) read(acct string, d doc) ([]byte, error) {
	b, err := os.ReadFile(s.file(acct, d))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}

func writeJSON(w http.ResponseWriter, b, empty []byte) {
	w.Header().Set("Content-Type", "application/json")
	if len(b) == 0 {
		b = empty
	}
	w.Write(b)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
