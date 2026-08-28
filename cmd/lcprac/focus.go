package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

// topicStats summarises the deck against your history, one row per topic. It
// is shared by stats and by drill -weak so both rank topics the same way.
func topicStats(set *drill.Set, store *progress.Store, now time.Time) []topicStat {
	var out []topicStat
	for _, t := range set.Topics() {
		ds := set.Filter(t, "", "")
		row := topicStat{Topic: t, Total: len(ds)}
		for _, d := range ds {
			if r, ok := store.Get(d.ID); ok {
				row.Tried++
				row.Seen += r.Seen
				row.Correct += r.Correct
				row.Assisted += r.Assisted
			}
			if store.Due(d.ID, now) {
				row.Due++
			}
		}
		out = append(out, row)
	}
	return out
}

// weakTopic picks the topic a session should spend itself on, and a line
// explaining the pick so an automatic choice is never silent.
func weakTopic(set *drill.Set, store *progress.Store, now time.Time) (string, string) {
	stats := topicStats(set, store, now)
	t := focus(stats)
	if t == "" {
		return "", ""
	}
	for _, row := range stats {
		if row.Topic != t {
			continue
		}
		if row.Seen >= minAttemptsToJudge {
			return t, fmt.Sprintf("weakest topic: %s at %d%% over %d attempts", t, row.Correct*100/row.Seen, row.Seen)
		}
		return t, fmt.Sprintf("least practised topic: %s, %d of %d drills untried", t, row.Total-row.Tried, row.Total)
	}
	return t, ""
}

// leechSet narrows the deck to the drills that keep beating you, so a sitting
// can be spent on exactly those. It returns nil when nothing qualifies.
func leechSet(set *drill.Set, store *progress.Store) (*drill.Set, error) {
	var ds []drill.Drill
	for _, r := range store.Leeches() {
		if d, ok := set.ByID(r.DrillID); ok {
			ds = append(ds, d)
		}
	}
	if len(ds) == 0 {
		return nil, nil
	}
	return drill.NewSet(ds)
}

// narrowDeck applies the -leech and -weak shortcuts, reporting what it did.
// Neither is fatal when there is nothing to act on: practice continues on the
// full deck rather than erroring out on an empty history.
func narrowDeck(set *drill.Set, store *progress.Store, now time.Time, w io.Writer, leech, weak bool, topic string) (*drill.Set, string, error) {
	if leech {
		sub, err := leechSet(set, store)
		if err != nil {
			return nil, "", err
		}
		if sub == nil {
			fmt.Fprintln(w, "nothing is beating you right now; running a normal session.")
		} else {
			fmt.Fprintf(w, "drilling the %d drill(s) you keep missing.\n", sub.Len())
			set = sub
		}
	}
	if weak && topic == "" {
		t, why := weakTopic(set, store, now)
		if t == "" {
			fmt.Fprintln(w, "not enough history to pick a weak topic; running a normal session.")
		} else {
			fmt.Fprintln(w, why)
			topic = t
		}
	} else if weak {
		fmt.Fprintf(w, "-weak ignored: -topic %s was given.\n", topic)
	}
	return set, topic, nil
}

// problemNumber returns the LeetCode number a ref leads with, or "" when it
// does not start with one. Refs read "LC 56 Merge Intervals".
func problemNumber(ref string) string {
	for _, f := range strings.Fields(ref) {
		if strings.EqualFold(f, "lc") {
			continue
		}
		if _, err := strconv.Atoi(strings.TrimSuffix(f, ".")); err == nil {
			return strings.TrimSuffix(f, ".")
		}
		return ""
	}
	return ""
}

// matchesProblem reports whether a ref answers the query. A bare number has
// to be the problem's own number, so "34" does not drag in LC 340; anything
// else is a case-insensitive substring of the title.
func matchesProblem(ref, query string) bool {
	q := strings.ToLower(strings.TrimSpace(query))
	q = strings.TrimSpace(strings.TrimPrefix(q, "lc"))
	if q == "" {
		return false
	}
	if _, err := strconv.Atoi(q); err == nil {
		n := problemNumber(ref)
		return n != "" && strings.TrimLeft(n, "0") == strings.TrimLeft(q, "0")
	}
	return strings.Contains(strings.ToLower(ref), q)
}

// problemDeck narrows the deck to the drills behind one real problem, so a
// warm-up can be aimed at the question you are about to attempt. Unlike
// -weak and -leech this is fatal when nothing matches: a typo'd problem
// should not quietly become a random session.
func problemDeck(set *drill.Set, query string, w io.Writer) (*drill.Set, error) {
	var ds []drill.Drill
	seen := map[string]bool{}
	var refs []string
	for _, d := range set.All() {
		hit := false
		for _, ref := range d.Refs {
			if !matchesProblem(ref, query) {
				continue
			}
			hit = true
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
		if hit {
			ds = append(ds, d)
		}
	}
	if len(ds) == 0 {
		return nil, fmt.Errorf("no drill names a problem matching %q; try `lcprac problems -all` to see them", query)
	}
	sort.Strings(refs)
	sub, err := drill.NewSet(ds)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(w, "warming up on %s: %d drill(s).\n", strings.Join(refs, ", "), sub.Len())
	return sub, nil
}
