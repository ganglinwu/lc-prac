package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

// cmdPace reports how long drills actually take you against what they were
// estimated at. Accuracy says whether you know a pattern; pace says whether
// you can produce it inside an interview's clock.
func cmdPace(args []string) error {
	fs := flag.NewFlagSet("pace", flag.ContinueOnError)
	n := fs.Int("n", 8, "how many drills to show (0 = all)")
	topic := fs.String("topic", "", "only this topic")
	all := fs.Bool("all", false, "show every timed drill, slowest first")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, _, err := drill.Combined()
	if err != nil {
		return err
	}
	store := openStore()
	rows := paceRows(set, store, *topic)
	if len(rows) == 0 {
		if store.Len() == 0 {
			fmt.Println("no history yet: run `lcprac drill` first.")
		} else {
			fmt.Println("nothing timed yet: pace is recorded from your next session on.")
		}
		return nil
	}
	if *all {
		*n = 0
	}
	writePace(os.Stdout, rows, *n)
	return nil
}

// paceRow is one drill's timing against its estimate.
type paceRow struct {
	Drill drill.Drill
	Seen  int
	// Avg is the mean time one attempt took.
	Avg time.Duration
	// Est is what the drill claims it should take. Zero when unestimated.
	Est time.Duration
}

// Over reports how far past the estimate an average attempt runs, as a
// percentage of the estimate. Zero when the drill carries no estimate.
func (p paceRow) Over() int {
	if p.Est <= 0 {
		return 0
	}
	return int((p.Avg - p.Est) * 100 / p.Est)
}

// paceRows joins timed history against the deck, slowest relative to estimate
// first. A record whose drill is gone is dropped: history outlives the deck.
func paceRows(set *drill.Set, store *progress.Store, topic string) []paceRow {
	var out []paceRow
	for _, r := range store.Timed() {
		d, ok := set.ByID(r.DrillID)
		if !ok || (topic != "" && d.Topic != topic) {
			continue
		}
		out = append(out, paceRow{
			Drill: d,
			Seen:  r.Seen,
			Avg:   time.Duration(r.AvgSeconds()) * time.Second,
			Est:   time.Duration(d.EstMinutes) * time.Minute,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Over() != out[j].Over() {
			return out[i].Over() > out[j].Over()
		}
		return out[i].Avg > out[j].Avg
	})
	return out
}

// writePace prints the slowest n rows and the totals underneath them, so the
// per-drill detail and the "does a sitting fit" answer arrive together.
func writePace(w io.Writer, rows []paceRow, n int) {
	var total, est time.Duration
	for _, p := range rows {
		total += p.Avg
		est += p.Est
	}
	shown := rows
	if n > 0 && len(shown) > n {
		shown = shown[:n]
	}
	fmt.Fprintf(w, "slowest first, average time per attempt\n\n")
	for _, p := range shown {
		fmt.Fprintf(w, "%-34s %-14s %6s  vs %3s est  %s (%d attempt(s))\n",
			trunc(p.Drill.Title, 34), p.Drill.Topic, short(p.Avg), short(p.Est), overMark(p), p.Seen)
	}
	avg := total / time.Duration(len(rows))
	fmt.Fprintf(w, "\n%d timed drill(s): %s each on average, against %s estimated.\n",
		len(rows), short(avg), short(est/time.Duration(len(rows))))
	if avg > 0 {
		fmt.Fprintf(w, "at that pace a 12 minute sitting fits about %d of them.\n", int(12*time.Minute/avg))
	}
}

// overMark renders the gap to the estimate in words, since a bare percentage
// reads the same whether you are 10 seconds or 4 minutes over.
func overMark(p paceRow) string {
	if p.Est <= 0 {
		return "unestimated"
	}
	if p.Avg <= p.Est {
		return "on pace"
	}
	return fmt.Sprintf("%d%% over", p.Over())
}

// short renders a duration the way a stopwatch would: 2m10s, 45s.
func short(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d/time.Second))
	}
	return fmt.Sprintf("%dm%02ds", int(d/time.Minute), int((d%time.Minute)/time.Second))
}

// trunc keeps a title inside its column without wrapping the row.
func trunc(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}
