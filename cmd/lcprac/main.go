// Command lcprac runs short LeetCode pattern drills: a 10-15 minute refresher
// instead of a full problem.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
	"github.com/ganglinwu/lc-prac/internal/runner"
	"github.com/ganglinwu/lc-prac/internal/session"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "lcprac:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cmd := "drill"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "drill":
		return cmdDrill(args)
	case "list":
		return cmdList(args)
	case "topics":
		return cmdTopics(args)
	case "stats":
		return cmdStats(args)
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `lcprac - short LeetCode pattern drills

  lcprac drill [-m 12] [-topic X] [-kind K] [-diff D] [-seed N] [-noretry] [-nolimit]
      Run a timed session that fits the minute budget (default 12). The clock
      is real: once the budget is spent no new drill starts. -nolimit disables
      that and lets the session run long.
  lcprac list [-topic X] [-kind K] [-diff D]
      List matching drills without running them.
  lcprac topics
      Show topics and how many drills each has.
  lcprac stats [-reset]
      Show what you have practised and what is due to come back.

  code drills compile what you write and run real tests against it: type the
  function and end with a line ".", or press e to open $EDITOR.

  kinds:        recall, choice, complexity, snippet, code
  difficulties: easy, medium
`)
}

// drillFlags registers the filters shared by drill and list.
func drillFlags(fs *flag.FlagSet) (topic, kind, diff *string) {
	topic = fs.String("topic", "", "only this topic")
	kind = fs.String("kind", "", "only this kind (recall, choice, complexity, snippet, code)")
	diff = fs.String("diff", "", "only this difficulty (easy, medium)")
	return topic, kind, diff
}

func cmdDrill(args []string) error {
	fs := flag.NewFlagSet("drill", flag.ContinueOnError)
	topic, kind, diff := drillFlags(fs)
	minutes := fs.Int("m", session.DefaultBudget, "minute budget for the session")
	seed := fs.Uint64("seed", 0, "seed for drill selection (0 = random)")
	shuffle := fs.Bool("shuffle", false, "ignore spaced repetition and pick at random")
	nosave := fs.Bool("nosave", false, "do not record this session in your history")
	noretry := fs.Bool("noretry", false, "do not re-ask missed drills at the end of the session")
	nolimit := fs.Bool("nolimit", false, "keep going past the minute budget instead of stopping")
	if err := fs.Parse(args); err != nil {
		return err
	}

	set, err := drill.Builtin()
	if err != nil {
		return err
	}
	store := openStore()
	if *seed == 0 {
		*seed = uint64(time.Now().UnixNano())
	}
	opts := session.Options{
		BudgetMinutes: *minutes,
		Topic:         *topic,
		Kind:          drill.Kind(*kind),
		Difficulty:    drill.Difficulty(*diff),
		Seed:          *seed,
	}
	if !*shuffle {
		now := time.Now()
		opts.Priority = func(d drill.Drill) int { return store.Priority(d.ID, now) }
	}
	s, err := session.Build(set, opts)
	if err != nil {
		return err
	}
	run := runner.New(os.Stdin, os.Stdout)
	run.RetryMisses = !*noretry
	run.NoTimeLimit = *nolimit
	rep, err := run.Run(s)
	if err != nil {
		return err
	}
	if *nosave {
		return nil
	}
	return saveResults(store, rep)
}

// openStore loads history, degrading to an unsaved in-memory store rather than
// blocking practice on a broken or unreadable file.
func openStore() *progress.Store {
	path, err := progress.DefaultPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, "lcprac: no history location:", err)
		return progress.New("")
	}
	store, err := progress.Load(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "lcprac: ignoring unreadable history:", err)
		return progress.New("")
	}
	return store
}

// saveResults folds the graded drills into history and reports what comes back
// when, so the scheduling is visible instead of implicit.
func saveResults(store *progress.Store, rep runner.Report) error {
	now := time.Now()
	recorded := 0
	for _, res := range rep.Results {
		if res.Skipped {
			continue
		}
		store.Record(res.Drill.ID, res.Correct, now)
		recorded++
	}
	if recorded == 0 {
		return nil
	}
	if err := store.Save(); err != nil {
		return err
	}
	soon := 0
	for _, res := range rep.Results {
		if !res.Skipped && !res.Correct {
			soon++
		}
	}
	fmt.Printf("\nsaved %d results to %s\n", recorded, store.Path())
	if soon > 0 {
		fmt.Printf("%d missed drills come back in ~10 min, ahead of everything else.\n", soon)
	}
	return nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	topic, kind, diff := drillFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, err := drill.Builtin()
	if err != nil {
		return err
	}
	matches := set.Filter(*topic, drill.Kind(*kind), drill.Difficulty(*diff))
	if len(matches) == 0 {
		return fmt.Errorf("no drills match those filters")
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Topic != matches[j].Topic {
			return matches[i].Topic < matches[j].Topic
		}
		return matches[i].ID < matches[j].ID
	})
	for _, d := range matches {
		fmt.Printf("%-28s %-16s %-11s %-7s %dm  %s\n", d.ID, d.Topic, d.Kind, d.Difficulty, d.EstMinutes, d.Title)
	}
	fmt.Printf("\n%d of %d drills\n", len(matches), set.Len())
	return nil
}

func cmdTopics(args []string) error {
	fs := flag.NewFlagSet("topics", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, err := drill.Builtin()
	if err != nil {
		return err
	}
	for _, t := range set.Topics() {
		ds := set.Filter(t, "", "")
		mins := 0
		for _, d := range ds {
			mins += d.EstMinutes
		}
		fmt.Printf("%-16s %2d drills  %2dm\n", t, len(ds), mins)
	}
	return nil
}

func cmdStats(args []string) error {
	fs := flag.NewFlagSet("stats", flag.ContinueOnError)
	reset := fs.Bool("reset", false, "erase all recorded history")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, err := drill.Builtin()
	if err != nil {
		return err
	}
	path, err := progress.DefaultPath()
	if err != nil {
		return err
	}
	if *reset {
		if err := progress.New(path).Save(); err != nil {
			return err
		}
		fmt.Printf("history cleared: %s\n", path)
		return nil
	}
	store, err := progress.Load(path)
	if err != nil {
		return err
	}
	if store.Len() == 0 {
		fmt.Printf("no history yet at %s\nrun `lcprac drill` first.\n", path)
		return nil
	}

	now := time.Now()
	var seen, correct, due int
	for _, t := range set.Topics() {
		var tSeen, tCorrect, tDue, tTouched int
		for _, d := range set.Filter(t, "", "") {
			r, ok := store.Get(d.ID)
			if ok {
				tTouched++
				tSeen += r.Seen
				tCorrect += r.Correct
			}
			if store.Due(d.ID, now) {
				tDue++
			}
		}
		seen += tSeen
		correct += tCorrect
		due += tDue
		fmt.Printf("%-16s %2d/%2d drills tried  %s  %2d due\n", t, tTouched, len(set.Filter(t, "", "")), pct(tCorrect, tSeen), tDue)
	}
	fmt.Printf("\ntotal: %s over %d attempts, %d of %d drills due now\n", pct(correct, seen), seen, due, set.Len())
	fmt.Printf("history: %s\n", path)
	return nil
}

// pct renders accuracy, or a placeholder when a topic is untouched.
func pct(correct, seen int) string {
	if seen == 0 {
		return "  --  "
	}
	return fmt.Sprintf("%3d%%  ", correct*100/seen)
}
