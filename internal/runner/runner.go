// Package runner drives a practice session interactively over a pair of streams.
package runner

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ganglinwu/lc-prac/internal/codecheck"
	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/session"
)

// endMarker terminates inline code entry. A lone dot is not valid Go, so it can
// never collide with a line the user meant to keep.
const endMarker = "."

// defaultCodeAttempts is how many times a code drill may be compiled before
// the answer is revealed. A typo should not cost you the drill, but an
// unbounded loop would eat the session's minutes.
const defaultCodeAttempts = 3

// Result records how one drill went.
type Result struct {
	Drill   drill.Drill
	Correct bool
	Skipped bool
	// Hints counts the nudges revealed before answering. A correct answer
	// with hints is real progress, but weaker than an unaided one.
	Hints int
	// Attempts counts compile-and-run tries on a code drill. Zero on every
	// other kind.
	Attempts int
	// Source is the last code submitted on a code drill, so it can be kept
	// and reread later. Empty on every other kind.
	Source  string
	Elapsed time.Duration
}

// Report is the tally for a whole session. Retries holds the second-pass
// attempts at drills missed on the first pass; they are reinforcement only and
// never counted in Score. Unasked holds drills the clock ran out on.
type Report struct {
	Results []Result
	Retries []Result
	Unasked []drill.Drill
	Elapsed time.Duration
	Budget  time.Duration
}

// Missed lists the drills answered wrong on the first pass.
func (r Report) Missed() []drill.Drill {
	var ds []drill.Drill
	for _, res := range r.Results {
		if !res.Skipped && !res.Correct {
			ds = append(ds, res.Drill)
		}
	}
	return ds
}

// Score returns correct and attempted counts; skipped drills are not attempted.
func (r Report) Score() (correct, attempted int) {
	for _, res := range r.Results {
		if res.Skipped {
			continue
		}
		attempted++
		if res.Correct {
			correct++
		}
	}
	return correct, attempted
}

// Runner presents drills and collects answers.
type Runner struct {
	in  *bufio.Scanner
	out io.Writer
	// Now is injectable so tests get deterministic timings.
	Now func() time.Time
	// RetryMisses re-asks missed drills once at the end of the session.
	RetryMisses bool
	// Budget is the wall-clock ceiling for the session. Zero means take it
	// from the session's minute budget; NoTimeLimit means do not stop at all.
	Budget time.Duration
	// NoTimeLimit lets a session run past its budget.
	NoTimeLimit bool
	// Grade compiles and runs a code drill. Nil means no Go toolchain is
	// available, so code drills fall back to self-grading.
	Grade func(context.Context, codecheck.Program) (codecheck.Result, error)
	// Editor composes code in an external editor. Nil disables the [e] option.
	Editor func(initial string) (string, error)
	// CodeAttempts caps the tries allowed on one code drill before the
	// working version is revealed. Zero means defaultCodeAttempts.
	CodeAttempts int
	// Notes holds your own wording on a drill by id, shown after the answer
	// so what you wrote comes back with the drill.
	Notes map[string]string
	// OnResult, if set, is called with each first-pass result as soon as it
	// is graded, so history survives a session that never reaches its end.
	// Second-pass retries are unscored and never reported here.
	OnResult func(Result)
}

// New builds a Runner over the given streams.
func New(in io.Reader, out io.Writer) *Runner {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	r := &Runner{in: sc, out: out, Now: time.Now, RetryMisses: true, Editor: EditInEditor}
	if codecheck.Available() {
		r.Grade = codecheck.Run
	}
	return r
}

// selfGrades reports whether this runner will ask the user to grade the drill.
// A code drill is machine-graded only when a toolchain was found.
func (r *Runner) selfGrades(d drill.Drill) bool {
	if d.Kind == drill.KindCode {
		return r.Grade == nil || d.Code == nil
	}
	return d.SelfGraded()
}

// budget resolves the wall-clock ceiling for a session. The estimate the
// session was packed against is only an estimate, so the real clock is what
// keeps a sitting inside the 10-15 minutes it promised.
func (r *Runner) budget(s session.Session) time.Duration {
	if r.NoTimeLimit {
		return 0
	}
	if r.Budget > 0 {
		return r.Budget
	}
	return time.Duration(s.BudgetMinutes) * time.Minute
}

// Run walks the session, returning once every drill is answered, the budget is
// spent, or the input stream ends.
func (r *Runner) Run(s session.Session) (Report, error) {
	start := r.Now()
	budget := r.budget(s)
	var deadline time.Time
	if budget > 0 {
		deadline = start.Add(budget)
	}
	fmt.Fprintf(r.out, "\n%d drills, about %d min (budget %d)\n", len(s.Drills), s.TotalMinutes(), s.BudgetMinutes)
	fmt.Fprintln(r.out, strings.Repeat("=", 60))

	rep := Report{Budget: budget}
	for i, d := range s.Drills {
		// Checked before starting, so a session overruns by at most the
		// drill in progress instead of being cut off mid-answer.
		if !deadline.IsZero() && !r.Now().Before(deadline) {
			rep.Unasked = append(rep.Unasked, s.Drills[i:]...)
			fmt.Fprintf(r.out, "\ntime is up (%s). %d left for next time.\n", budget.Round(time.Second), len(rep.Unasked))
			break
		}
		res, err := r.runOne(fmt.Sprintf("%d/%d", i+1, len(s.Drills)), d, deadline)
		if err == io.EOF {
			break
		}
		if err != nil {
			return rep, err
		}
		rep.Results = append(rep.Results, res)
		if r.OnResult != nil {
			r.OnResult(res)
		}
	}
	if err := r.retryPass(&rep, deadline); err != nil {
		return rep, err
	}
	rep.Elapsed = r.Now().Sub(start)
	r.printSummary(rep)
	return rep, nil
}

// retryPass re-asks every missed drill once, straight away. Scheduling puts a
// miss 10 minutes out, which is a later sitting, so without this you never see
// the drill again while the explanation is still fresh.
// The pass gets a quarter of the budget beyond the deadline: reinforcement is
// worth a small overrun, but not an unbounded one.
func (r *Runner) retryPass(rep *Report, deadline time.Time) error {
	missed := rep.Missed()
	if !r.RetryMisses || len(missed) == 0 {
		return nil
	}
	if !deadline.IsZero() {
		deadline = deadline.Add(rep.Budget / 4)
	}
	fmt.Fprintf(r.out, "\n"+strings.Repeat("-", 60)+"\nsecond pass: %d missed, once more while it is fresh\n", len(missed))
	for i, d := range missed {
		if !deadline.IsZero() && !r.Now().Before(deadline) {
			fmt.Fprintf(r.out, "\nout of time for the rest of the second pass.\n")
			return nil
		}
		res, err := r.runOne(fmt.Sprintf("retry %d/%d", i+1, len(missed)), d, deadline)
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		rep.Retries = append(rep.Retries, res)
	}
	return nil
}

// deadline bounds the drill's own retry loop (zero means no clock); the drill
// in progress is never cut off mid-answer.
func (r *Runner) runOne(label string, d drill.Drill, deadline time.Time) (Result, error) {
	started := r.Now()
	fmt.Fprintf(r.out, "\n[%s] %s  (%s, %s, ~%dm)\n", label, d.Title, d.Topic, d.Difficulty, d.EstMinutes)
	fmt.Fprintf(r.out, "\n%s\n", d.Prompt)
	for i, c := range d.Choices {
		fmt.Fprintf(r.out, "  %d) %s\n", i+1, c)
	}

	machineGraded := d.Kind == drill.KindCode && !r.selfGrades(d)
	prompt := func() {
		switch {
		case machineGraded:
			r.codePrompt(d)()
		case r.selfGrades(d):
			fmt.Fprintf(r.out, "\nThink it through, then press enter to reveal (%s): ", answerKeys(d))
		default:
			fmt.Fprintf(r.out, "\nYour answer (%s): ", answerKeys(d))
		}
	}
	if machineGraded {
		fmt.Fprintf(r.out, "\n%s\n", d.Code.Stub)
	}
	input, hints, err := r.readAnswer(d, prompt)
	if err != nil {
		return Result{}, err
	}

	res := Result{Drill: d, Hints: hints}
	if strings.EqualFold(strings.TrimSpace(input), "s") {
		res.Skipped = true
		fmt.Fprintf(r.out, "\nskipped. answer: %s\n", d.Answer)
		res.Elapsed = r.Now().Sub(started)
		return res, nil
	}

	if machineGraded {
		out, err := r.gradeCode(d, input, deadline)
		if err != nil {
			return Result{}, err
		}
		res.Correct = out.correct
		res.Attempts = out.attempts
		res.Source = out.source
		res.Hints += out.hints
		fmt.Fprintf(r.out, "\n%s\n", d.Explanation)
	} else if r.selfGrades(d) {
		fmt.Fprintf(r.out, "\nanswer: %s\n\n%s\n", d.Answer, d.Explanation)
		fmt.Fprintf(r.out, "\nDid you have it? [y/N]: ")
		verdict, err := r.readLine()
		if err != nil {
			return Result{}, err
		}
		res.Correct = strings.EqualFold(strings.TrimSpace(verdict), "y")
	} else {
		res.Correct = checkAnswer(d, input)
		if res.Correct {
			fmt.Fprintf(r.out, "\ncorrect.\n")
		} else {
			fmt.Fprintf(r.out, "\nnot quite. answer: %s\n", d.Answer)
		}
		fmt.Fprintf(r.out, "\n%s\n", d.Explanation)
	}
	if n := r.Notes[d.ID]; n != "" {
		fmt.Fprintf(r.out, "\nyour note: %s\n", n)
	}
	if len(d.Refs) > 0 {
		fmt.Fprintf(r.out, "\nrelated: %s\n", strings.Join(d.Refs, ", "))
	}
	res.Elapsed = r.Now().Sub(started)
	if est := time.Duration(d.EstMinutes) * time.Minute; est > 0 && res.Elapsed > 2*est {
		fmt.Fprintf(r.out, "\npace: %s on a ~%dm drill.\n", res.Elapsed.Round(time.Second), d.EstMinutes)
	}
	return res, nil
}

// readAnswer prompts until the user gives something that is not a hint
// request, returning their line and how many hints they burned. Hints are only
// intercepted on drills that have them, so "h" stays a valid answer elsewhere.
func (r *Runner) readAnswer(d drill.Drill, prompt func()) (string, int, error) {
	used := 0
	for {
		prompt()
		line, err := r.readLine()
		if err != nil {
			return "", used, err
		}
		if len(d.Hints) == 0 || !strings.EqualFold(strings.TrimSpace(line), "h") {
			return line, used, nil
		}
		if used >= len(d.Hints) {
			fmt.Fprintf(r.out, "\nthat was the last hint.\n")
			continue
		}
		fmt.Fprintf(r.out, "\nhint %d/%d: %s\n", used+1, len(d.Hints), d.Hints[used])
		used++
	}
}

// answerKeys lists the single-letter keys the prompt should advertise. The
// hint key is only mentioned on drills that carry hints, so "h" stays an
// ordinary answer everywhere else.
func answerKeys(d drill.Drill) string {
	if len(d.Hints) > 0 {
		return "h for a hint, s to skip"
	}
	return "s to skip"
}

// codePrompt returns the code-entry prompt, reused for every attempt.
func (r *Runner) codePrompt(d drill.Drill) func() {
	return func() {
		if r.Editor != nil {
			fmt.Fprintf(r.out, "\ne to open $EDITOR, or type your code and end with a line \"%s\" (%s): ", endMarker, answerKeys(d))
		} else {
			fmt.Fprintf(r.out, "\nType your code and end with a line \"%s\" (%s): ", endMarker, answerKeys(d))
		}
	}
}

// codeOutcome is what one code drill cost: whether it ended up passing, how
// many compiles it took and how many hints were burned along the way.
type codeOutcome struct {
	correct  bool
	attempts int
	hints    int
	// source is the last version compiled, kept so the session can save your
	// own working answer alongside the model one.
	source string
}

// codeAttempts is the try ceiling for one code drill.
func (r *Runner) codeAttempts() int {
	if r.CodeAttempts > 0 {
		return r.CodeAttempts
	}
	return defaultCodeAttempts
}

// gradeCode compiles the user's source against the drill's tests, and on a
// failure offers another try instead of revealing the answer straight away:
// a missing return or a typo is worth fixing yourself, and reading the failing
// test output is most of the value. first is the line already consumed by
// runOne. The working version is shown once the tries run out or the user
// asks for it, or the session clock runs out mid-drill.
func (r *Runner) gradeCode(d drill.Drill, first string, deadline time.Time) (codeOutcome, error) {
	var out codeOutcome
	max := r.codeAttempts()
	seed := d.Code.Stub
	line := first
	for {
		src, err := r.collectSource(d, line, seed, &out)
		if err != nil {
			return out, err
		}
		if strings.TrimSpace(src) == "" {
			fmt.Fprintln(r.out, "\nnothing to compile.")
			r.revealCode(d)
			return out, nil
		}
		out.attempts++
		out.source = src

		fmt.Fprintln(r.out, "\ncompiling and running the tests...")
		res, err := r.Grade(context.Background(), codecheck.Program{
			Preamble: d.Code.Preamble,
			Source:   src,
			Tests:    d.Code.Tests,
		})
		if err != nil {
			fmt.Fprintf(r.out, "\ncould not grade: %v\n", err)
			return out, nil
		}
		if res.Passed {
			fmt.Fprintln(r.out, "\nall tests pass.")
			out.correct = true
			return out, nil
		}
		if res.TimedOut {
			fmt.Fprintln(r.out, "\ntimed out, so something never terminates.")
		}
		fmt.Fprintf(r.out, "\ntests failed:\n%s\n", res.Output)
		if out.attempts >= max {
			fmt.Fprintf(r.out, "\nthat was try %d of %d.\n", out.attempts, max)
			r.revealCode(d)
			return out, nil
		}
		// Another try would run on borrowed time: the session clock already
		// stopped, so offer the answer instead of a fourth compile.
		if !deadline.IsZero() && !r.Now().Before(deadline) {
			fmt.Fprintln(r.out, "\nout of time for another try.")
			r.revealCode(d)
			return out, nil
		}
		fmt.Fprintf(r.out, "\nr to fix it (%d %s left), anything else to give up: ", max-out.attempts, plural(max-out.attempts, "try", "tries"))
		again, hints, err := r.readAnswer(d, func() {})
		out.hints += hints
		if err != nil || !strings.EqualFold(strings.TrimSpace(again), "r") {
			r.revealCode(d)
			return out, nil
		}
		// Next try starts from what you just wrote, not the stub.
		seed, line = src, ""
	}
}

// collectSource gathers one attempt's Go source. line is the entry already
// read (empty on a retry, where the prompt is issued here); seed is what the
// editor opens on.
func (r *Runner) collectSource(d drill.Drill, line, seed string, out *codeOutcome) (string, error) {
	if line == "" {
		got, hints, err := r.readAnswer(d, r.codePrompt(d))
		out.hints += hints
		if err != nil {
			return "", err
		}
		line = got
	}
	if strings.EqualFold(strings.TrimSpace(line), "e") && r.Editor != nil {
		edited, err := r.Editor(seed)
		if err != nil {
			fmt.Fprintf(r.out, "\neditor failed: %v\n", err)
			return "", nil
		}
		return edited, nil
	}
	rest, err := r.readUntilMarker()
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.Join(append([]string{line}, rest...), "\n"), nil
}

func (r *Runner) revealCode(d drill.Drill) {
	fmt.Fprintf(r.out, "\nworking version:\n%s\n", d.Answer)
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// readUntilMarker consumes lines up to the end marker, returning what it read
// even when the stream ends first (Ctrl-D is a legitimate way to finish).
func (r *Runner) readUntilMarker() ([]string, error) {
	var lines []string
	for {
		line, err := r.readLine()
		if err != nil {
			return lines, err
		}
		if strings.TrimSpace(line) == endMarker {
			return lines, nil
		}
		lines = append(lines, line)
	}
}

// EditInEditor opens $EDITOR on a temp file seeded with the stub and returns
// what was saved.
func EditInEditor(initial string) (string, error) {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	dir, err := os.MkdirTemp("", "lcprac-edit-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "solution.go")
	if err := os.WriteFile(path, []byte(initial+"\n"), 0o600); err != nil {
		return "", err
	}
	cmd := exec.Command(editor, path)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s: %w", editor, err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (r *Runner) printSummary(rep Report) {
	correct, attempted := rep.Score()
	fmt.Fprintln(r.out, "\n"+strings.Repeat("=", 60))
	budget := ""
	if rep.Budget > 0 {
		budget = fmt.Sprintf(" of a %s budget", rep.Budget.Round(time.Second))
	}
	fmt.Fprintf(r.out, "%d/%d correct in %s%s\n", correct, attempted, rep.Elapsed.Round(time.Second), budget)
	for _, res := range rep.Results {
		mark := "x"
		switch {
		case res.Skipped:
			mark = "-"
		case res.Correct && res.Hints > 0:
			mark = "~"
		case res.Correct:
			mark = "+"
		}
		tries := ""
		if res.Attempts > 1 {
			tries = fmt.Sprintf(" %d tries", res.Attempts)
		}
		fmt.Fprintf(r.out, "  %s %s (%s) %s%s\n", mark, res.Drill.Title, res.Drill.Topic, res.Elapsed.Round(time.Second), tries)
	}
	for _, d := range rep.Unasked {
		fmt.Fprintf(r.out, "  . %s (%s) not reached\n", d.Title, d.Topic)
	}
	if len(rep.Retries) > 0 {
		var got int
		for _, res := range rep.Retries {
			if res.Correct && !res.Skipped {
				got++
			}
		}
		fmt.Fprintf(r.out, "second pass: %d/%d (not scored, they still come back)\n", got, len(rep.Retries))
	}
}

func (r *Runner) readLine() (string, error) {
	if !r.in.Scan() {
		if err := r.in.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return r.in.Text(), nil
}

// checkAnswer grades an auto-gradable drill. Choice drills accept either the
// option number or the option text; complexity drills compare loosely so
// "O(n log k)" and "o(nlogk)" both pass.
func checkAnswer(d drill.Drill, input string) bool {
	in := strings.TrimSpace(input)
	if in == "" {
		return false
	}
	if d.Kind == drill.KindChoice {
		if i, err := strconv.Atoi(in); err == nil {
			if i >= 1 && i <= len(d.Choices) {
				return d.Choices[i-1] == d.Answer
			}
			return false
		}
	}
	return normalize(in) == normalize(d.Answer)
}

// normalize strips everything except letters and digits, lowercased, so
// formatting differences in a complexity answer do not count as wrong.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
