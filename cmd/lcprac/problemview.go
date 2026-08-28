package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

// resolveProblemRef turns a query into one problem name, searching the deck's
// refs and your attempt log as one namespace so a gap logged with
// `attempt -new` can be read back the same way a drilled problem can.
func resolveProblemRef(set *drill.Set, store *progress.Store, query string) (string, error) {
	// matchesProblem drops a leading "lc", so a bare "lc" matches nothing.
	if strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(query)), "lc")) == "" {
		return "", fmt.Errorf("name the problem, e.g. `lcprac problems 56`")
	}
	seen := map[string]bool{}
	var refs []string
	collect := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] || !matchesProblem(ref, query) {
			return
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	for _, d := range set.All() {
		for _, r := range d.Refs {
			collect(r)
		}
	}
	for _, a := range store.Attempts() {
		collect(a.Ref)
	}
	sort.Strings(refs)
	switch len(refs) {
	case 0:
		return "", fmt.Errorf("no problem matching %q; try `lcprac problems -all` to see them", query)
	case 1:
		return refs[0], nil
	default:
		return "", fmt.Errorf("%q matches %d problems (%s); be more specific", query, len(refs), strings.Join(refs, "; "))
	}
}

// writeProblemDetail is everything the app knows about one real problem: the
// drills cut from it and how they have gone, plus every time you sat down and
// attempted the problem itself.
func writeProblemDetail(w io.Writer, ref string, set *drill.Set, store *progress.Store, now time.Time) {
	var row problemRow
	for _, r := range problemRows(set, store, "", now) {
		if r.Ref == ref {
			row = r
			break
		}
	}
	fmt.Fprintf(w, "%s\n%s\n\n", ref, problemMark(row))

	ds := drillsFor(set, ref)
	if len(ds) == 0 {
		fmt.Fprintf(w, "no drill in the deck covers it (`lcprac add -problem %s` to write one).\n", coverFlag(row))
	} else {
		fmt.Fprintf(w, "drills behind it (%s)\n", strings.Join(row.Topics, ", "))
		for _, d := range ds {
			fmt.Fprintf(w, "  %-24s %-11s %s\n", trunc(d.ID, 24), d.Kind, drillMark(store, d, now))
			if note := store.Note(d.ID); note != "" {
				fmt.Fprintf(w, "      note: %s\n", note)
			}
			if sol, ok := store.Solution(d.ID); ok {
				fmt.Fprintf(w, "      your code kept (%s, %s)\n", passLabel(sol.Passed), sol.At.Format("2 Jan 2006"))
			}
		}
	}

	if as := attemptsFor(store, ref); len(as) > 0 {
		fmt.Fprint(w, "\nyour attempts at the real thing\n")
		for _, a := range as {
			fmt.Fprintf(w, "  %-11s %s\n", a.At.Format("2 Jan 2006"), a.Result)
			if a.Note != "" {
				fmt.Fprintf(w, "      %s\n", a.Note)
			}
		}
	} else {
		fmt.Fprintf(w, "\nyou have not logged a real attempt (`lcprac attempt %s` after you do).\n", coverFlag(row))
	}

	// A problem with no drill already got its `add` line above.
	if row.Drills > 0 {
		fmt.Fprint(w, "\n"+nextUp(row))
	}
}

// drillsFor lists the deck's drills that name this problem, in deck order.
func drillsFor(set *drill.Set, ref string) []drill.Drill {
	var out []drill.Drill
	for _, d := range set.All() {
		for _, r := range d.Refs {
			if strings.EqualFold(strings.TrimSpace(r), ref) {
				out = append(out, d)
				break
			}
		}
	}
	return out
}

// attemptsFor is your log for one problem, newest first.
func attemptsFor(store *progress.Store, ref string) []progress.Attempt {
	var out []progress.Attempt
	for _, a := range store.Attempts() {
		if a.Ref == ref {
			out = append(out, a)
		}
	}
	return out
}

// drillMark is one drill's standing in words: how it has gone and whether the
// scheduler wants it back.
func drillMark(store *progress.Store, d drill.Drill, now time.Time) string {
	r, ok := store.Get(d.ID)
	if !ok || r.Seen == 0 {
		return "not drilled yet"
	}
	mark := fmt.Sprintf("%d/%d right", r.Correct, r.Seen)
	if store.Due(d.ID, now) {
		mark += ", due"
	}
	if r.IsLeech() {
		mark += ", leech"
	}
	return mark
}

func passLabel(passed bool) string {
	if passed {
		return "passed"
	}
	return "failed"
}
