// Package codecheck compiles a snippet of Go against a drill's test file and
// reports whether it passes, so a code drill is graded by the toolchain rather
// than by the user's judgement of a model answer.
package codecheck

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DefaultTimeout bounds a single grading run. A drill that loops forever should
// fail fast, not hang the session.
const DefaultTimeout = 30 * time.Second

// Program is one gradable unit: the user's source plus the support code and
// test file that surround it.
type Program struct {
	// Preamble is compiled into the same package, ahead of Source.
	Preamble string
	// Source is what the user wrote.
	Source string
	// Tests is a complete test file body, everything after the package clause.
	Tests string
}

// Result is the outcome of one grading run.
type Result struct {
	// Passed is true only when the test binary built and every test passed.
	Passed bool
	// Output is the combined build and test output, trimmed.
	Output string
	// TimedOut is true when the run was killed by the timeout.
	TimedOut bool
}

// ErrNoToolchain means the Go toolchain is not on PATH, so the caller should
// fall back to self-grading instead of failing the drill.
var ErrNoToolchain = errors.New("codecheck: no go toolchain on PATH")

// Available reports whether grading is possible on this machine.
func Available() bool {
	_, err := exec.LookPath("go")
	return err == nil
}

// Run builds a throwaway module around the program and runs `go test` in it.
// A non-nil error means grading itself broke; a failing solution is a Result
// with Passed false.
func Run(ctx context.Context, p Program) (Result, error) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		return Result{}, ErrNoToolchain
	}

	dir, err := os.MkdirTemp("", "lcprac-code-")
	if err != nil {
		return Result{}, fmt.Errorf("codecheck: temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	// The user's source gets its own file so their own import block is still
	// legal, which it would not be sitting underneath the preamble's decls.
	files := map[string]string{
		"go.mod":           "module lcdrill\n\ngo 1.21\n",
		"support.go":       joinFile(p.Preamble),
		"solution.go":      joinFile(p.Source),
		"solution_test.go": joinFile(p.Tests),
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			return Result{}, fmt.Errorf("codecheck: write %s: %w", name, err)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, goBin, "test", "-count=1", ".")
	cmd.Dir = dir
	// The generated module has no requirements, so grading must never reach the
	// network; an offline proxy keeps a stray import a fast compile error.
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=mod", "GOPROXY=off", "GO111MODULE=on")
	out, err := cmd.CombinedOutput()

	res := Result{Output: strings.TrimSpace(string(out))}
	switch {
	case ctx.Err() != nil:
		// Deadline or an upstream cancel: either way the solution never
		// finished, which is a failed drill and not a grading breakdown.
		res.TimedOut = errors.Is(ctx.Err(), context.DeadlineExceeded)
		if res.Output == "" {
			res.Output = fmt.Sprintf("run stopped early (%v), suspect an infinite loop", ctx.Err())
		}
	case err == nil:
		res.Passed = true
	default:
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			return res, fmt.Errorf("codecheck: run go test: %w", err)
		}
	}
	return res, nil
}

// joinFile assembles a package file out of the non-empty parts.
func joinFile(parts ...string) string {
	body := []string{"package lcdrill"}
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			body = append(body, strings.TrimSpace(p))
		}
	}
	return strings.Join(body, "\n\n") + "\n"
}
