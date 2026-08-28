package main

import (
	"fmt"
	"io"
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
