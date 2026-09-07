package main

import (
	"flag"
	"fmt"
	"io"
	"os"

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
	if *setURL != "" || *setToken != "" {
		return saveSyncConfig(os.Stdout, path, cfg, *setURL, *setToken)
	}
	if *show {
		return showSync(os.Stdout, path, cfg)
	}
	return doSync(os.Stdout, cfg, *dry)
}

// saveSyncConfig updates only the fields you named, so setting a new token
// does not make you retype the url.
func saveSyncConfig(w io.Writer, path string, cfg synccli.Config, url, token string) error {
	if url != "" {
		cfg.URL = url
	}
	if token != "" {
		cfg.Token = token
	}
	if err := synccli.SaveConfig(path, cfg); err != nil {
		return err
	}
	fmt.Fprintf(w, "syncing to %s\nsaved to %s\n", cfg.URL, path)
	return nil
}

// showSync reports the destination without the token, so the output is safe to
// paste when something is not working.
func showSync(w io.Writer, path string, cfg synccli.Config) error {
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(w, "sync is not set up: %v\n", err)
		return nil
	}
	fmt.Fprintf(w, "server: %s\ntoken:  set (%d chars)\nconfig: %s\n", cfg.URL, len(cfg.Token), path)
	return nil
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
	merged, delta, err := synccli.Sync(synccli.NewClient(cfg), local)
	if err != nil {
		return err
	}
	if !delta.Changed() {
		fmt.Fprintf(w, "already in sync with %s (%d drills).\n", cfg.URL, merged.Len())
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
}
