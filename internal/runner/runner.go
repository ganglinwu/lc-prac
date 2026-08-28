// Package runner drives a practice session interactively over a pair of streams.
package runner

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/session"
)

// Result records how one drill went.
type Result struct {
	Drill   drill.Drill
	Correct bool
	Skipped bool
	Elapsed time.Duration
}

// Report is the tally for a whole session.
type Report struct {
	Results []Result
	Elapsed time.Duration
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
}

// New builds a Runner over the given streams.
func New(in io.Reader, out io.Writer) *Runner {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &Runner{in: sc, out: out, Now: time.Now}
}

// Run walks the session, returning once every drill is answered or the input
// stream ends.
func (r *Runner) Run(s session.Session) (Report, error) {
	start := r.Now()
	fmt.Fprintf(r.out, "\n%d drills, about %d min (budget %d)\n", len(s.Drills), s.TotalMinutes(), s.BudgetMinutes)
	fmt.Fprintln(r.out, strings.Repeat("=", 60))

	var rep Report
	for i, d := range s.Drills {
		res, err := r.runOne(i+1, len(s.Drills), d)
		if err == io.EOF {
			break
		}
		if err != nil {
			return rep, err
		}
		rep.Results = append(rep.Results, res)
	}
	rep.Elapsed = r.Now().Sub(start)
	r.printSummary(rep)
	return rep, nil
}

func (r *Runner) runOne(n, total int, d drill.Drill) (Result, error) {
	started := r.Now()
	fmt.Fprintf(r.out, "\n[%d/%d] %s  (%s, %s, ~%dm)\n", n, total, d.Title, d.Topic, d.Difficulty, d.EstMinutes)
	fmt.Fprintf(r.out, "\n%s\n", d.Prompt)
	for i, c := range d.Choices {
		fmt.Fprintf(r.out, "  %d) %s\n", i+1, c)
	}

	if d.SelfGraded() {
		fmt.Fprintf(r.out, "\nThink it through, then press enter to reveal (s to skip): ")
	} else {
		fmt.Fprintf(r.out, "\nYour answer (s to skip): ")
	}
	input, err := r.readLine()
	if err != nil {
		return Result{}, err
	}

	res := Result{Drill: d}
	if strings.EqualFold(strings.TrimSpace(input), "s") {
		res.Skipped = true
		fmt.Fprintf(r.out, "\nskipped. answer: %s\n", d.Answer)
		res.Elapsed = r.Now().Sub(started)
		return res, nil
	}

	if d.SelfGraded() {
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
	if len(d.Refs) > 0 {
		fmt.Fprintf(r.out, "\nrelated: %s\n", strings.Join(d.Refs, ", "))
	}
	res.Elapsed = r.Now().Sub(started)
	return res, nil
}

func (r *Runner) printSummary(rep Report) {
	correct, attempted := rep.Score()
	fmt.Fprintln(r.out, "\n"+strings.Repeat("=", 60))
	fmt.Fprintf(r.out, "%d/%d correct in %s\n", correct, attempted, rep.Elapsed.Round(time.Second))
	for _, res := range rep.Results {
		mark := "x"
		switch {
		case res.Skipped:
			mark = "-"
		case res.Correct:
			mark = "+"
		}
		fmt.Fprintf(r.out, "  %s %s (%s)\n", mark, res.Drill.Title, res.Drill.Topic)
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
