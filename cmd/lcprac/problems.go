package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

// cmdProblems maps the deck back onto the real LeetCode problems its drills
// were cut from, so a refresher can end with "now go do this one for real".
func cmdProblems(args []string) error {
	fs := flag.NewFlagSet("problems", flag.ContinueOnError)
	n := fs.Int("n", 10, "how many problems to show (0 = all)")
	topic := fs.String("topic", "", "only problems whose drills are in this topic")
	all := fs.Bool("all", false, "show every problem the deck points at")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, _, err := drill.Combined()
	if err != nil {
		return err
	}
	store := openStore()
	rows := problemRows(set, store, *topic, time.Now())
	if len(rows) == 0 {
		if *topic != "" {
			fmt.Printf("no drill in %q names a problem.\n", *topic)
		} else {
			fmt.Println("no drill in the deck names a problem yet.")
		}
		return nil
	}
	if *all {
		*n = 0
	}
	writeProblems(os.Stdout, rows, *n)
	return nil
}

// problemRow is one problem the deck refers to, joined to your history on
// every drill that names it.
type problemRow struct {
	Ref     string
	Topics  []string
	Drills  int
	Tried   int
	Seen    int
	Correct int
	// Attempt is your last go at the real problem, and Fresh says that go was
	// a solve recent enough that the problem is not worth redoing yet.
	Attempt *progress.Attempt
	Fresh   bool
}

// Accuracy is the percentage right across every attempt on the backing
// drills. It is meaningless until one of them has been tried.
func (p problemRow) Accuracy() int {
	if p.Seen == 0 {
		return 0
	}
	return p.Correct * 100 / p.Seen
}

// tier orders the list the way you would work through it: the patterns you
// get wrong, then the ones you have never touched, then the settled ones.
func (p problemRow) tier() int {
	if p.Attempt != nil && p.Attempt.Result != progress.Passed {
		return 0
	}
	// A problem no drill covers stays a gap even after a clean solve: there
	// is nothing in the deck to keep it fresh.
	if p.Drills == 0 {
		return 1
	}
	if p.Attempt != nil && p.Fresh {
		return 3
	}
	switch {
	case p.Seen > 0 && p.Accuracy() < 100:
		return 0
	case p.Seen == 0:
		return 1
	default:
		return 2
	}
}

// problemRows folds the deck's refs into one row per problem. A drill with no
// refs contributes nothing: it is a pattern with no single problem behind it.
func problemRows(set *drill.Set, store *progress.Store, topic string, now time.Time) []problemRow {
	byRef := map[string]*problemRow{}
	topics := map[string]map[string]int{}
	for _, d := range set.Filter(topic, "", "") {
		for _, ref := range d.Refs {
			ref = strings.TrimSpace(ref)
			if ref == "" {
				continue
			}
			row := byRef[ref]
			if row == nil {
				row = &problemRow{Ref: ref}
				byRef[ref] = row
				topics[ref] = map[string]int{}
			}
			row.Drills++
			topics[ref][d.Topic]++
			if r, ok := store.Get(d.ID); ok && r.Seen > 0 {
				row.Tried++
				row.Seen += r.Seen
				row.Correct += r.Correct
			}
		}
	}
	// Problems you logged with `attempt -new` have no drill behind them at
	// all; they belong in the list as coverage gaps rather than being
	// invisible. A topic filter is about the deck, so it excludes them.
	if topic == "" {
		for _, a := range store.Attempts() {
			if byRef[a.Ref] == nil {
				byRef[a.Ref] = &problemRow{Ref: a.Ref}
				topics[a.Ref] = map[string]int{}
			}
		}
	}
	out := make([]problemRow, 0, len(byRef))
	for ref, row := range byRef {
		if a, ok := store.LastAttempt(ref); ok {
			row.Attempt = &a
			row.Fresh = a.Result == progress.Passed && now.Sub(a.At) < staleAfter
		}
		for t := range topics[ref] {
			row.Topics = append(row.Topics, t)
		}
		// Most-backed topic first, so the topic named in the next-up line is
		// the one the problem mostly drills.
		sort.Slice(row.Topics, func(i, j int) bool {
			a, b := row.Topics[i], row.Topics[j]
			if topics[ref][a] != topics[ref][b] {
				return topics[ref][a] > topics[ref][b]
			}
			return a < b
		})
		out = append(out, *row)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.tier() != b.tier() {
			return a.tier() < b.tier()
		}
		if a.tier() == 0 && a.Accuracy() != b.Accuracy() {
			return a.Accuracy() < b.Accuracy()
		}
		if a.Drills != b.Drills {
			return a.Drills > b.Drills
		}
		return a.Ref < b.Ref
	})
	return out
}

// writeProblems prints the top n rows and a tally over all of them, so a
// truncated list still reports how much of the deck you have covered.
func writeProblems(w io.Writer, rows []problemRow, n int) {
	shown := rows
	if n > 0 && len(shown) > n {
		shown = shown[:n]
	}
	fmt.Fprint(w, "problems behind your drills, weakest first\n\n")
	for _, p := range shown {
		fmt.Fprintf(w, "%-48s %-24s %d drill(s)  %s\n",
			trunc(p.Ref, 48), trunc(strings.Join(p.Topics, ", "), 24), p.Drills, problemMark(p))
	}
	var weak, untried, solid int
	for _, p := range rows {
		switch p.tier() {
		case 0:
			weak++
		case 1:
			untried++
		default:
			solid++
		}
	}
	fmt.Fprintf(w, "\n%d problem(s) behind the deck: %d shaky, %d not drilled yet, %d solid.\n",
		len(rows), weak, untried, solid)
	if logged := attemptedCount(rows); logged > 0 {
		fmt.Fprintf(w, "%d of them you have attempted for real (`lcprac attempt` to log another).\n", logged)
	}
	if gaps := uncoveredCount(rows); gaps > 0 {
		verb := "have"
		if gaps == 1 {
			verb = "has"
		}
		fmt.Fprintf(w, "%d of them %s no drill behind it yet (`lcprac add -problem <n>` to write one).\n", gaps, verb)
	}
	if len(shown) > 0 {
		fmt.Fprint(w, nextUp(shown[0]))
	}
}

// problemMark says where you stand on a problem in words, since a bare
// percentage cannot distinguish never-tried from never-right.
func problemMark(p problemRow) string {
	mark := fmt.Sprintf("%d%% over %d attempt(s)", p.Accuracy(), p.Seen)
	switch {
	case p.Drills == 0:
		mark = "no drill covers it"
	case p.Seen == 0:
		mark = "not drilled yet"
	}
	if p.Attempt != nil {
		mark += fmt.Sprintf("; %s it %s", p.Attempt.Result, p.Attempt.At.Format("2 Jan"))
	}
	return mark
}

// staleAfter is how long a solved problem stays settled before it is worth
// attempting again, matching the top of the drill scheduler's ladder.
const staleAfter = 60 * 24 * time.Hour

// nextUp is the one line telling you what to do with the top row: warm up on
// its drills, or write one when the problem has none.
func nextUp(top problemRow) string {
	if top.Drills == 0 {
		return fmt.Sprintf("next up: `lcprac add -problem %s` a drill for %s, nothing in the deck covers it.\n", coverFlag(top), top.Ref)
	}
	return fmt.Sprintf("next up: attempt %s for real, or `lcprac drill %s` to warm up first.\n", top.Ref, warmUpFlag(top))
}

// coverFlag is what to pass `add -problem`: the number when the ref carries
// one, otherwise the whole quoted name.
func coverFlag(p problemRow) string {
	if n := problemNumber(p.Ref); n != "" {
		return n
	}
	return strconv.Quote(p.Ref)
}

// uncoveredCount is how many problems you have attempted for real that no
// drill in the deck touches.
func uncoveredCount(rows []problemRow) int {
	n := 0
	for _, p := range rows {
		if p.Drills == 0 {
			n++
		}
	}
	return n
}

// warmUpFlag is the drill invocation that warms you up for a problem: by
// number when the ref carries one, otherwise by its main topic.
func warmUpFlag(p problemRow) string {
	if n := problemNumber(p.Ref); n != "" {
		return "-problem " + n
	}
	return "-topic " + p.Topics[0]
}

// attemptedCount is how many of the problems carry a logged real attempt.
func attemptedCount(rows []problemRow) int {
	n := 0
	for _, p := range rows {
		if p.Attempt != nil {
			n++
		}
	}
	return n
}
