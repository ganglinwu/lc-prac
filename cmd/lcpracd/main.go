// Command lcpracd is the sync server behind `lcprac sync`. It keeps the
// canonical practice history and merges whatever a machine pushes into it.
//
// It listens on loopback by default and expects Caddy in front for TLS, which
// is how the rest of the box is already wired.
package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ganglinwu/lc-prac/internal/syncsrv"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "lcpracd:", err)
		os.Exit(1)
	}
}

func run() error {
	addr := envOr("LCPRAC_SYNC_ADDR", "127.0.0.1:8090")
	dir := envOr("LCPRAC_SYNC_DATA", "/var/lib/lcpracd")
	tokens, err := parseTokens(os.Getenv("LCPRAC_SYNC_TOKENS"))
	if err != nil {
		return err
	}
	srv, err := syncsrv.New(dir, tokens)
	if err != nil {
		return err
	}
	hs := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	fmt.Fprintf(os.Stderr, "lcpracd: listening on %s, data in %s, %d account(s)\n", addr, dir, len(tokens))
	return hs.ListenAndServe()
}

// parseTokens reads the "account:token,account:token" form used by the unit
// file's environment, so adding an identity is one edit and a restart.
func parseTokens(s string) (map[string]string, error) {
	out := map[string]string{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		acct, tok, ok := strings.Cut(pair, ":")
		if !ok || strings.TrimSpace(acct) == "" || strings.TrimSpace(tok) == "" {
			return nil, fmt.Errorf("LCPRAC_SYNC_TOKENS entry %q is not account:token", pair)
		}
		out[strings.TrimSpace(tok)] = strings.TrimSpace(acct)
	}
	if len(out) == 0 {
		return nil, errors.New("LCPRAC_SYNC_TOKENS is empty; set it to account:token")
	}
	return out, nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
