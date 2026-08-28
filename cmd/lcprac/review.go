package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

// cmdReview prints drills and their answers to read, not to answer: the ones
// you keep missing, or whichever ids you name.
func cmdReview(args []string) error {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	topic := fs.String("topic", "", "only this topic")
	n := fs.Int("n", 5, "how many drills to show (0 = all)")
	all := fs.Bool("all", false, "show every drill you have ever missed, not just the sticky ones")
	code := fs.Bool("code", false, "show the code drills you have written a solution for")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, _, err := drill.Combined()
	if err != nil {
		return err
	}
	store := openStore()

	if ids := fs.Args(); len(ids) > 0 {
		for i, id := range ids {
			d, ok := set.ByID(id)
			if !ok {
				return fmt.Errorf("no drill with id %q (try `lcprac list`)", id)
			}
			r, _ := store.Get(id)
			writeReview(os.Stdout, d, r, i > 0)
		}
		return nil
	}

	picked := pickReview(set, store, *topic, *n, *all, *code)
	if len(picked) == 0 {
		switch {
		case store.Len() == 0:
			fmt.Println("no history yet: run `lcprac drill` first.")
		case *code:
			fmt.Println("no saved code yet: solve a code drill and your version is kept here.")
		default:
			fmt.Println("nothing to review: no drill has beaten you enough times to stick out.")
			fmt.Println("`lcprac review -all` shows everything you have ever missed.")
		}
		return nil
	}
	for i, p := range picked {
		writeReview(os.Stdout, p.drill, p.record, i > 0)
	}
	fmt.Printf("\n%d drill(s) to reread. `lcprac review <id>` shows any drill by id.\n", len(picked))
	return nil
}

// reviewItem pairs a drill with why it made the list.
type reviewItem struct {
	drill  drill.Drill
	record progress.Record
}

// pickReview chooses what to reread: leeches by default, anything ever missed
// with -all, your own saved code with -code. Records with no matching drill (a
// deleted user drill) are dropped.
func pickReview(set *drill.Set, store *progress.Store, topic string, n int, all, code bool) []reviewItem {
	var out []reviewItem
	switch {
	case code:
		for _, r := range store.Solved() {
			out = append(out, reviewItem{record: r})
		}
	case all:
		for _, r := range store.Records() {
			if r.Misses() > 0 {
				out = append(out, reviewItem{record: r})
			}
		}
	default:
		for _, r := range store.Leeches() {
			out = append(out, reviewItem{record: r})
		}
	}
	var kept []reviewItem
	for _, it := range out {
		d, ok := set.ByID(it.record.DrillID)
		if !ok || (topic != "" && d.Topic != topic) {
			continue
		}
		it.drill = d
		kept = append(kept, it)
	}
	if n > 0 && len(kept) > n {
		kept = kept[:n]
	}
	return kept
}

// writeReview renders one drill with its answer laid open, since review is
// reading rather than a quiz.
func writeReview(w io.Writer, d drill.Drill, r progress.Record, spaced bool) {
	if spaced {
		fmt.Fprintln(w)
	}
	fmt.Fprintf(w, "%s  [%s/%s]", d.ID, d.Topic, d.Kind)
	if r.Seen > 0 {
		fmt.Fprintf(w, "  missed %d of %d", r.Misses(), r.Seen)
	}
	fmt.Fprintf(w, "\n%s\n\n", d.Title)
	fmt.Fprintf(w, "%s\n", indent(d.Prompt))
	for i, c := range d.Choices {
		fmt.Fprintf(w, "  %d. %s\n", i+1, c)
	}
	fmt.Fprintf(w, "\nanswer:\n%s\n", indent(d.Answer))
	fmt.Fprintf(w, "why:\n%s\n", indent(d.Explanation))
	if r.Solution != nil {
		state := "did not pass"
		if r.Solution.Passed {
			state = "passed"
		}
		fmt.Fprintf(w, "your code (%s, %s):\n%s\n", state, r.Solution.At.Format("2 Jan 2006"), indent(r.Solution.Source))
	}
	if r.Note != "" {
		fmt.Fprintf(w, "your note:\n%s\n", indent(r.Note))
	}
	if len(d.Refs) > 0 {
		fmt.Fprintf(w, "refs: %s\n", strings.Join(d.Refs, ", "))
	}
}

// indent shifts a block right so a multi-line answer stays visually attached
// to its heading.
func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n")
}
