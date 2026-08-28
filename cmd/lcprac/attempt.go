package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

// cmdAttempt logs a go at a real LeetCode problem, closing the loop the other
// way round from `problems`: the drills say what you should attempt, this says
// what actually happened when you did.
func cmdAttempt(args []string) error {
	res, rest, err := attemptResult(args)
	if err != nil {
		return err
	}
	store := openStore()
	if len(rest) == 0 {
		writeAttempts(os.Stdout, store.Attempts(), 0)
		return nil
	}
	set, _, err := drill.Combined()
	if err != nil {
		return err
	}
	ref, err := resolveProblem(set, rest[0])
	if err != nil {
		return err
	}
	note := strings.Join(rest[1:], " ")
	store.LogAttempt(ref, res, note, time.Now())
	if err := store.Save(); err != nil {
		return err
	}
	writeLogged(os.Stdout, ref, res, note)
	return nil
}

// attemptResult pulls the outcome flag out from anywhere in the argument list,
// since flag.Parse stops at the first positional and the problem comes first.
// No flag means solved: saying you attempted something is usually saying you
// got it, and the confirmation names the other two.
func attemptResult(args []string) (progress.Result, []string, error) {
	res := progress.Passed
	given := false
	var rest []string
	for _, a := range args {
		var r progress.Result
		switch a {
		case "-solved", "--solved":
			r = progress.Passed
		case "-partial", "--partial":
			r = progress.Partial
		case "-failed", "--failed":
			r = progress.Failed
		default:
			if strings.HasPrefix(a, "-") && len(rest) == 0 {
				return res, nil, fmt.Errorf("unknown flag %q; use -solved, -partial or -failed", a)
			}
			rest = append(rest, a)
			continue
		}
		if given && r != res {
			return res, nil, fmt.Errorf("pick one of -solved, -partial or -failed")
		}
		res, given = r, true
	}
	return res, rest, nil
}

// resolveProblem turns a query into exactly one of the deck's refs, so the log
// keys on the same string `problems` ranks by. An ambiguous title is an error
// rather than a guess: logging the wrong problem is worse than retyping.
func resolveProblem(set *drill.Set, query string) (string, error) {
	seen := map[string]bool{}
	var refs []string
	for _, d := range set.All() {
		for _, ref := range d.Refs {
			if !matchesProblem(ref, query) || seen[ref] {
				continue
			}
			seen[ref] = true
			refs = append(refs, ref)
		}
	}
	sort.Strings(refs)
	switch len(refs) {
	case 0:
		return "", fmt.Errorf("no drill names a problem matching %q; try `lcprac problems -all` to see them", query)
	case 1:
		return refs[0], nil
	default:
		return "", fmt.Errorf("%q matches %d problems (%s); be more specific", query, len(refs), strings.Join(refs, "; "))
	}
}

// writeLogged confirms what went down and, when it did not go well, points at
// the drills behind that problem.
func writeLogged(w io.Writer, ref string, res progress.Result, note string) {
	fmt.Fprintf(w, "logged %s as %s.\n", ref, res)
	if note != "" {
		fmt.Fprintf(w, "  note: %s\n", note)
	}
	if res == progress.Passed {
		fmt.Fprintln(w, "(use -partial or -failed if that is not how it went.)")
		return
	}
	if n := problemNumber(ref); n != "" {
		fmt.Fprintf(w, "next up: `lcprac drill -problem %s` to drill the pattern behind it.\n", n)
	}
}

// writeAttempts prints the log newest first, which is the record of what you
// have actually sat down and solved rather than only drilled.
func writeAttempts(w io.Writer, as []progress.Attempt, n int) {
	if len(as) == 0 {
		fmt.Fprint(w, "no real problems logged yet.\n\nlog one with `lcprac attempt 56` after you attempt it.\n")
		return
	}
	shown := as
	if n > 0 && len(shown) > n {
		shown = shown[:n]
	}
	fmt.Fprint(w, "real problems you have attempted, newest first\n\n")
	for _, a := range shown {
		fmt.Fprintf(w, "%-11s %-8s %s\n", a.At.Format("2 Jan 2006"), a.Result, trunc(a.Ref, 48))
		if a.Note != "" {
			fmt.Fprintf(w, "            %s\n", a.Note)
		}
	}
	var solved, other int
	for _, a := range as {
		if a.Result == progress.Passed {
			solved++
		} else {
			other++
		}
	}
	fmt.Fprintf(w, "\n%d attempt(s): %d solved, %d not yet.\n", len(as), solved, other)
}
