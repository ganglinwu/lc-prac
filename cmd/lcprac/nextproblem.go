package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
	"github.com/ganglinwu/lc-prac/internal/runner"
)

// suggestProblem ends a sitting by naming one real problem to go attempt. The
// drills are the warm-up; without this line the loop back to LeetCode is a
// separate command you have to remember to run.
func suggestProblem(w io.Writer, set *drill.Set, store *progress.Store, rep runner.Report, now time.Time) {
	drilled, missed := sessionRefs(rep)
	if len(drilled) == 0 {
		return
	}
	p, wasMissed, ok := pickSessionProblem(problemRows(set, store, "", now), drilled, missed)
	if !ok {
		return
	}
	writeSessionProblem(w, p, wasMissed)
}

// sessionRefs collects the problems behind the drills just answered, and the
// subset behind the ones answered wrong. Skipped drills teach nothing, so
// they name no problem.
func sessionRefs(rep runner.Report) (drilled, missed map[string]bool) {
	drilled, missed = map[string]bool{}, map[string]bool{}
	for _, res := range rep.Results {
		if res.Skipped {
			continue
		}
		for _, ref := range res.Drill.Refs {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			drilled[ref] = true
			if !res.Correct {
				missed[ref] = true
			}
		}
	}
	return drilled, missed
}

// pickSessionProblem takes the ranked problem list and keeps the best row the
// sitting actually warmed you up for, preferring one whose drill you just got
// wrong. A problem solved for real recently is skipped: the point is to spend
// the rest of the sitting on something unsettled.
func pickSessionProblem(rows []problemRow, drilled, missed map[string]bool) (problemRow, bool, bool) {
	for _, want := range []map[string]bool{missed, drilled} {
		for _, p := range rows {
			if want[p.Ref] && !p.Fresh {
				return p, missed[p.Ref], true
			}
		}
	}
	return problemRow{}, false, false
}

// writeSessionProblem is the two line handoff from drills to the real thing.
func writeSessionProblem(w io.Writer, p problemRow, missed bool) {
	why := "you just drilled its pattern"
	if missed {
		why = "you missed a drill behind it just now"
	}
	fmt.Fprintf(w, "\nnow attempt %s for real: %s.\n", p.Ref, why)
	fmt.Fprintf(w, "log how it goes with `lcprac attempt %s`.\n", coverFlag(p))
}
