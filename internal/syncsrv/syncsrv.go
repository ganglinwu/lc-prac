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
	return mux
}

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request) {
	acct, ok := s.authenticate(r)
	if !ok {
		w.Header().Set("WWW-Authenticate", `Bearer realm="lcprac"`)
		httpError(w, http.StatusUnauthorized, "bad or missing token")
		return
	}
	switch r.Method {
	case http.MethodGet:
		s.handleGet(w, acct)
	case http.MethodPost:
		s.handlePost(w, r, acct)
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

func (s *Server) handleGet(w http.ResponseWriter, acct string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := s.read(acct)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "reading history")
		return
	}
	writeJSON(w, b)
}

func (s *Server) handlePost(w http.ResponseWriter, r *http.Request, acct string) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		httpError(w, http.StatusRequestEntityTooLarge, "history too large")
		return
	}
	client, err := progress.Decode("", body)
	if err != nil {
		httpError(w, http.StatusBadRequest, "history is not valid lcprac JSON")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	stored, err := s.read(acct)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "reading history")
		return
	}
	server, err := progress.Decode(s.file(acct), stored)
	if err != nil {
		httpError(w, http.StatusInternalServerError, "stored history is corrupt")
		return
	}
	merged := server.Merge(client)
	if err := merged.Save(); err != nil {
		httpError(w, http.StatusInternalServerError, "writing history")
		return
	}
	out, err := merged.Encode()
	if err != nil {
		httpError(w, http.StatusInternalServerError, "encoding history")
		return
	}
	writeJSON(w, out)
}

// file is where an account's history lives. Account names are validated in
// New, so this cannot point outside the data directory.
func (s *Server) file(acct string) string {
	return filepath.Join(s.path, acct+".json")
}

// read returns the stored bytes, treating a missing file as no history yet.
func (s *Server) read(acct string) ([]byte, error) {
	b, err := os.ReadFile(s.file(acct))
	if os.IsNotExist(err) {
		return nil, nil
	}
	return b, err
}

func writeJSON(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "application/json")
	if len(b) == 0 {
		b = []byte("{}\n")
	}
	w.Write(b)
}

func httpError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
