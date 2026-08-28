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
	case "mine":
		return cmdMine(args)
	case "add":
		return cmdAdd(args)
	case "review":
		return cmdReview(args)
	case "note":
		return cmdNote(args)
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

  lcprac drill [-m 12] [-topic X] [-kind K] [-diff D] [-seed N] [-noretry] [-nolimit] [-tries 3] [-builtin] [-weak] [-leech]
      Run a timed session that fits the minute budget (default 12). The clock
      is real: once the budget is spent no new drill starts. -nolimit disables
      that and lets the session run long. A failed code drill offers another
      try (3 by default, -tries changes it) before revealing the answer.
      -weak aims the session at your weakest topic and -leech at the drills
      you keep missing; both fall back to a normal session if history is thin.
  lcprac list [-topic X] [-kind K] [-diff D] [-builtin]
      List matching drills without running them. Yours are marked *.
  lcprac topics
      Show topics and how many drills each has.
  lcprac stats [-reset]
      Show what you have practised, your streak and recent sessions, and
      what is due to come back.
  lcprac review [-n 5] [-topic X] [-all] [id...]
      Reread the drills you keep missing, answers shown. Name ids to read
      those instead; -all widens it to everything you have ever missed.
  lcprac note [id] [your words] [-clear]
      Keep your own wording on a drill. It is shown when the drill comes back
      and in review. With no id it lists every note you have written.
  lcprac add [-file mine.json]
      Write a new drill of your own by answering a few prompts. It is appended
      to a file in your drills directory and is in the deck immediately.
  lcprac mine [-init]
      Show where your own drill files live and what they add. -init writes a
      commented example you can copy.

  your own drills are any *.json files in that directory, in the same shape as
  the builtin deck. They are added to the deck automatically; one that reuses a
  builtin id replaces it. Pass -builtin to drill or list to ignore them.

  answers are saved as you go, so stopping halfway keeps what you did. Ctrl-C
  ends the sitting cleanly and still logs it towards your streak.

  press h at any prompt for a hint on drills that carry one. Solving with a
  hint still counts, but the drill keeps its place in the schedule.

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

// loadDeck returns the deck to work from: builtin plus your own drills unless
// asked for builtin only.
func loadDeck(builtinOnly bool) (*drill.Set, error) {
	if builtinOnly {
		return drill.Builtin()
	}
	set, _, err := drill.Combined()
	return set, err
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
	tries := fs.Int("tries", 0, "compile tries allowed per code drill (0 = 3)")
	builtinOnly := fs.Bool("builtin", false, "use only the builtin deck, ignoring your own drills")
	weak := fs.Bool("weak", false, "spend the session on your weakest topic")
	leech := fs.Bool("leech", false, "spend the session on the drills you keep missing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	set, err := loadDeck(*builtinOnly)
	if err != nil {
		return err
	}
	store := openStore()
	if *weak || *leech {
		set, *topic, err = narrowDeck(set, store, time.Now(), os.Stdout, *leech, *weak, *topic)
		if err != nil {
			return err
		}
	}
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
	run.Notes = notesFor(store)
	run.RetryMisses = !*noretry
	run.NoTimeLimit = *nolimit
	run.CodeAttempts = *tries
	var live *liveProgress
	if !*nosave {
		live = newLive(store, os.Stdout, time.Now)
		run.OnResult = live.record
		defer catchInterrupt(live)()
	}
	rep, err := run.Run(s)
	if err != nil {
		return err
	}
	if *nosave {
		return nil
	}
	return saveResults(live, rep)
}

// notesFor collects your written notes by drill id, so the runner can show
// yours next to the canned explanation when a drill comes back.
func notesFor(store *progress.Store) map[string]string {
	notes := map[string]string{}
	for _, r := range store.Noted() {
		notes[r.DrillID] = r.Note
	}
	return notes
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

// outcome maps a graded drill onto the scheduler's view of it. A hint used is
// what separates a solve from an assist.
func outcome(res runner.Result) progress.Outcome {
	switch {
	case !res.Correct:
		return progress.Missed
	case res.Hints > 0:
		return progress.Assisted
	default:
		return progress.Solved
	}
}

// saveResults closes out a finished session: the drills themselves were
// already recorded as they were answered, so this logs the sitting and reports
// what comes back when, making the scheduling visible instead of implicit.
func saveResults(live *liveProgress, rep runner.Report) error {
	live.mu.Lock()
	defer live.mu.Unlock()
	if live.recorded == 0 {
		return nil
	}
	live.logSession(rep.Elapsed)
	fmt.Fprintf(live.out, "\nsaved %d results to %s\n", live.recorded, live.store.Path())
	if n := live.store.DayStreak(live.now()); n > 1 {
		fmt.Fprintf(live.out, "practice streak: %d days.\n", n)
	}
	if live.hinted > 0 {
		fmt.Fprintf(live.out, "%d solved with a hint, so they hold their place instead of climbing the ladder.\n", live.hinted)
	}
	if live.missed > 0 {
		fmt.Fprintf(live.out, "%d missed drills come back in ~10 min, ahead of everything else.\n", live.missed)
	}
	return nil
}

func cmdList(args []string) error {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	topic, kind, diff := drillFlags(fs)
	builtinOnly := fs.Bool("builtin", false, "use only the builtin deck, ignoring your own drills")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, err := loadDeck(*builtinOnly)
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
		fmt.Printf("%s%-28s %-16s %-11s %-7s %dm  %s\n", mark(d), d.ID, d.Topic, d.Kind, d.Difficulty, d.EstMinutes, d.Title)
	}
	fmt.Printf("\n%d of %d drills\n", len(matches), set.Len())
	return nil
}

func cmdTopics(args []string) error {
	fs := flag.NewFlagSet("topics", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, _, err := drill.Combined()
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
	set, _, err := drill.Combined()
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
	var seen, correct, assisted, due int
	stats := topicStats(set, store, now)
	for _, row := range stats {
		seen += row.Seen
		correct += row.Correct
		assisted += row.Assisted
		due += row.Due
		fmt.Printf("%-16s %2d/%2d drills tried  %s  %2d due\n", row.Topic, row.Tried, row.Total, pct(row.Correct, row.Seen), row.Due)
	}
	fmt.Printf("\ntotal: %s over %d attempts, %d of %d drills due now\n", pct(correct, seen), seen, due, set.Len())
	if assisted > 0 {
		fmt.Printf("%d of those correct answers needed a hint.\n", assisted)
	}
	if n := len(store.Leeches()); n > 0 {
		fmt.Printf("%d drill(s) keep beating you: lcprac review, or lcprac drill -leech\n", n)
	}
	if next := focus(stats); next != "" {
		fmt.Printf("next up: lcprac drill -weak  (that is %s right now)\n", next)
	}
	printSessions(store, now)
	fmt.Printf("history: %s\n", path)
	return nil
}

// printSessions shows the recent sittings and the streak, which is the part of
// the history that says whether the habit is holding.
func printSessions(store *progress.Store, now time.Time) {
	recent := store.RecentSessions(5)
	if len(recent) == 0 {
		return
	}
	fmt.Printf("\nrecent sessions\n")
	for _, s := range recent {
		fmt.Printf("  %s  %2dm  %2d drills  %s\n", s.At.Local().Format("Mon 02 Jan 15:04"), s.Minutes, s.Attempted, pct(s.Correct, s.Attempted))
	}
	if n := store.DayStreak(now); n > 0 {
		fmt.Printf("streak: %d day(s) in a row\n", n)
	}
}

// mark flags a drill you wrote yourself, so your deck is visible in a listing.
func mark(d drill.Drill) string {
	if d.Source == drill.SourceUser {
		return "*"
	}
	return " "
}

// wholeMinutes rounds a sitting to at least one minute, so a fast session is
// still logged as time spent rather than as zero.
func wholeMinutes(d time.Duration) int {
	m := int((d + 30*time.Second) / time.Minute)
	if m < 1 {
		m = 1
	}
	return m
}

// topicStat is one row of the stats table, kept as data so the focus pick can
// be tested without printing.
type topicStat struct {
	Topic                                      string
	Tried, Total, Seen, Correct, Assisted, Due int
}

// minAttemptsToJudge is how many graded attempts a topic needs before its
// accuracy is taken seriously; below it a single miss would rank worst.
const minAttemptsToJudge = 3

// focus suggests what to practise next: the weakest topic you have real data
// on, else the one with the most drills you have never tried.
func focus(stats []topicStat) string {
	best, bestAcc := "", 101
	for _, t := range stats {
		if t.Seen < minAttemptsToJudge {
			continue
		}
		acc := t.Correct * 100 / t.Seen
		if acc < bestAcc {
			best, bestAcc = t.Topic, acc
		}
	}
	if best != "" {
		return best
	}
	most := 0
	for _, t := range stats {
		if untried := t.Total - t.Tried; untried > most {
			best, most = t.Topic, untried
		}
	}
	return best
}

// pct renders accuracy, or a placeholder when a topic is untouched.
func pct(correct, seen int) string {
	if seen == 0 {
		return "  --  "
	}
	return fmt.Sprintf("%3d%%  ", correct*100/seen)
}
