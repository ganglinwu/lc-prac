package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

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
	rows := problemRows(set, store, *topic)
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
func problemRows(set *drill.Set, store *progress.Store, topic string) []problemRow {
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
	out := make([]problemRow, 0, len(byRef))
	for ref, row := range byRef {
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
	if len(shown) > 0 {
		top := shown[0]
		fmt.Fprintf(w, "next up: attempt %s for real, or `lcprac drill -topic %s` first.\n", top.Ref, top.Topics[0])
	}
}

// problemMark says where you stand on a problem in words, since a bare
// percentage cannot distinguish never-tried from never-right.
func problemMark(p problemRow) string {
	if p.Seen == 0 {
		return "not drilled yet"
	}
	return fmt.Sprintf("%d%% over %d attempt(s)", p.Accuracy(), p.Seen)
}
