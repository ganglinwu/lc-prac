package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
	"github.com/ganglinwu/lc-prac/internal/synccli"
)

// cmdSync carries this machine's history to the server and back, so practice
// done on the laptop is there on the desktop. The merge is symmetric, so
// running it twice in a row is a no-op rather than a doubling.
func cmdSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	setURL := fs.String("set-url", "", "save the sync server url and exit")
	setToken := fs.String("set-token", "", "save the sync token and exit")
	show := fs.Bool("status", false, "show where this machine syncs to")
	dry := fs.Bool("n", false, "show what a sync would bring without saving it")
	auto := fs.String("auto", "", "turn syncing around each drill session on or off")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path, err := synccli.ConfigPath()
	if err != nil {
		return err
	}
	cfg, err := synccli.LoadConfig(path)
	if err != nil {
		return err
	}
	if *setURL != "" || *setToken != "" || *auto != "" {
		return saveSyncConfig(os.Stdout, path, cfg, *setURL, *setToken, *auto)
	}
	if *show {
		return showSync(os.Stdout, path, cfg)
	}
	return doSync(os.Stdout, cfg, *dry)
}

// saveSyncConfig updates only the fields you named, so setting a new token
// does not make you retype the url.
func saveSyncConfig(w io.Writer, path string, cfg synccli.Config, url, token, auto string) error {
	if url != "" {
		cfg.URL = url
	}
	if token != "" {
		cfg.Token = token
	}
	if auto != "" {
		on, err := parseOnOff(auto)
		if err != nil {
			return err
		}
		cfg.Auto = &on
	}
	if err := synccli.SaveConfig(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(w, "syncing to %s (%s around drill sessions)\nsaved to %s\n", cfg.URL, onOff(cfg.AutoEnabled()), path)
	return nil
}

// parseOnOff takes the words the help text uses, not only Go's booleans.
func parseOnOff(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "on", "true", "yes", "1":
		return true, nil
	case "off", "false", "no", "0":
		return false, nil
	}
	return false, fmt.Errorf("-auto wants on or off, not %q", v)
}

// showSync reports the destination without the token, so the output is safe to
// paste when something is not working.
func showSync(w io.Writer, path string, cfg synccli.Config) error {
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(w, "sync is not set up: %v\n", err)
		return nil
	}
	fmt.Fprintf(w, "server: %s\ntoken:  set (%d chars)\nauto:   %s around drill sessions\nconfig: %s\n",
		cfg.URL, len(cfg.Token), onOff(cfg.AutoEnabled()), path)
	return nil
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// doSync runs the round trip and says what came back. Nothing is written
// unless the merge actually differs from what is already here.
func doSync(w io.Writer, cfg synccli.Config, dry bool) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	p, err := progress.DefaultPath()
	if err != nil {
		return err
	}
	local, err := progress.Load(p)
	if err != nil {
		return err
	}
	client := synccli.NewClient(cfg)
	merged, delta, err := synccli.Sync(client, local)
	if err != nil {
		return err
	}
	if delta.NewWritten, delta.UpdatedWritten, err = syncOwnDrills(client, !dry); err != nil {
		return err
	}
	if !delta.Changed() {
		// Sync always pushes first, so nothing coming back means the server
		// now holds this machine's history, not that the call did nothing.
		fmt.Fprintf(w, "in sync with %s (%d drills); nothing new to bring back.\n", cfg.URL, merged.Len())
		return nil
	}
	printDelta(w, delta)
	if dry {
		fmt.Fprintln(w, "-n given, so nothing was written.")
		return nil
	}
	if err := merged.Save(); err != nil {
		return err
	}
	fmt.Fprintf(w, "history now at %d drills: %s\n", merged.Len(), p)
	return nil
}

// syncOwnDrills carries the drills you wrote yourself. It is a second document
// on the same server, so it needs its own round trip; apply is false for a dry
// run, which counts what would arrive without writing any file.
func syncOwnDrills(c *synccli.Client, apply bool) (int, int, error) {
	dir, err := drill.UserDir()
	if err != nil {
		// No home directory means nowhere to keep your own drills, which is
		// not a reason to fail a history sync that already worked.
		return 0, 0, nil
	}
	return synccli.SyncDrills(c, dir, apply)
}

// printDelta spells out what the server had that this machine did not.
func printDelta(w io.Writer, d synccli.Delta) {
	fmt.Fprintln(w, "pulled from the server:")
	if d.NewDrills > 0 {
		fmt.Fprintf(w, "  %d drill(s) this machine had never seen\n", d.NewDrills)
	}
	if d.UpdatedDrills > 0 {
		fmt.Fprintf(w, "  %d drill(s) with newer history elsewhere\n", d.UpdatedDrills)
	}
	if d.NewSessions > 0 {
		fmt.Fprintf(w, "  %d session(s) practised on another machine\n", d.NewSessions)
	}
	if d.NewAttempts > 0 {
		fmt.Fprintf(w, "  %d problem attempt(s)\n", d.NewAttempts)
	}
	if d.NewWritten > 0 {
		fmt.Fprintf(w, "  %d drill(s) you wrote on another machine\n", d.NewWritten)
	}
	if d.UpdatedWritten > 0 {
		fmt.Fprintf(w, "  %d drill(s) you edited on another machine\n", d.UpdatedWritten)
	}
}

// autoSync is the sync a drill session does for you. It never returns an
// error: a session must not fail because a server is down, so a problem is a
// line on stderr and practice carries on with local history.
func autoSync(w io.Writer, quiet bool) {
	cfg, err := autoConfig()
	if err != nil || cfg == nil {
		if err != nil {
			fmt.Fprintln(os.Stderr, "lcprac: skipping auto sync:", err)
		}
		return
	}
	p, err := progress.DefaultPath()
	if err != nil {
		return
	}
	local, err := progress.Load(p)
	if err != nil {
		return
	}
	client := synccli.NewAutoClient(*cfg)
	merged, delta, err := synccli.Sync(client, local)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lcprac: sync skipped:", err)
		return
	}
	if delta.NewWritten, delta.UpdatedWritten, err = syncOwnDrills(client, true); err != nil {
		// The history half already worked, so this is a warning rather than a
		// reason to leave the merge unsaved.
		fmt.Fprintln(os.Stderr, "lcprac: your own drills were not synced:", err)
	}
	if !delta.Changed() {
		return
	}
	if err := merged.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "lcprac: sync not saved:", err)
		return
	}
	if quiet {
		return
	}
	printDelta(w, delta)
}

// autoConfig returns the config only when this machine is set up and has not
// turned auto sync off; nil means stay quiet rather than complain.
func autoConfig() (*synccli.Config, error) {
	path, err := synccli.ConfigPath()
	if err != nil {
		return nil, err
	}
	cfg, err := synccli.LoadConfig(path)
	if err != nil {
		return nil, err
	}
	if !cfg.Ready() || !cfg.AutoEnabled() {
		return nil, nil
	}
	return &cfg, nil
}
