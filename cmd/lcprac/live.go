package main

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync"
	"time"

	"github.com/ganglinwu/lc-prac/internal/progress"
	"github.com/ganglinwu/lc-prac/internal/runner"
)

// interruptCode is the conventional exit status for a process killed by SIGINT.
const interruptCode = 130

// liveProgress folds each graded drill into history the moment it is answered
// instead of at the end of the sitting, so a session abandoned with Ctrl-C (or
// a closed terminal) keeps the drills already done.
type liveProgress struct {
	store *progress.Store
	out   io.Writer
	now   func() time.Time

	mu       sync.Mutex
	started  time.Time
	logged   bool
	recorded int
	correct  int
	hinted   int
	missed   int
	skipped  int
}

func newLive(store *progress.Store, out io.Writer, now func() time.Time) *liveProgress {
	return &liveProgress{store: store, out: out, now: now, started: now()}
}

// record grades one drill into the store and saves straight away. A failed
// save is reported once but never stops the session: practice matters more
// than bookkeeping.
func (l *liveProgress) record(res runner.Result) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if res.Skipped {
		l.skipped++
		return
	}
	l.store.RecordOutcome(res.Drill.ID, outcome(res), l.now())
	l.recorded++
	switch {
	case !res.Correct:
		l.missed++
	case res.Hints > 0:
		l.correct++
		l.hinted++
	default:
		l.correct++
	}
	if err := l.store.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "lcprac: could not save progress:", err)
	}
}

// logSession appends the sitting to the session log, once. Both the normal end
// of a session and an interrupt go through here, so a half session still
// counts towards the streak.
func (l *liveProgress) logSession(elapsed time.Duration) {
	if l.logged || l.recorded == 0 {
		return
	}
	l.logged = true
	l.store.AddSession(progress.Session{
		At:        l.now(),
		Minutes:   wholeMinutes(elapsed),
		Attempted: l.recorded,
		Correct:   l.correct,
		Assisted:  l.hinted,
		Skipped:   l.skipped,
	})
	if err := l.store.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "lcprac: could not save progress:", err)
	}
}

// interrupt closes out a session cut short: the drills answered so far are
// already on disk, so only the sitting itself still needs logging.
func (l *liveProgress) interrupt() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.logSession(l.now().Sub(l.started))
	if l.recorded == 0 {
		fmt.Fprintf(l.out, "\n\nstopped before any drill was graded, nothing recorded.\n")
		return
	}
	fmt.Fprintf(l.out, "\n\nstopped early: %d of %d graded, %d saved to %s\n",
		l.correct, l.recorded, l.recorded, l.store.Path())
}

// catchInterrupt makes Ctrl-C end the sitting cleanly rather than dropping the
// session log. Per-drill results are already saved by record; this exists so
// the streak and the recent-sessions view do not lose the sitting. It returns
// a stop function for the normal path.
func catchInterrupt(l *liveProgress) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			l.interrupt()
			os.Exit(interruptCode)
		case <-done:
		}
	}()
	return func() {
		signal.Stop(ch)
		close(done)
	}
}
