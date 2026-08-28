package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

// errCancelled ends the interview without writing, either on Ctrl-D or on an
// explicit no at the confirmation.
var errCancelled = errors.New("cancelled")

// defaultAddFile is where a drill lands unless -file says otherwise. One file
// keeps everything you write in one place; the loader reads every *.json anyway.
const defaultAddFile = "mine.json"

func cmdAdd(args []string) error {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	file := fs.String("file", defaultAddFile, "file in your drills directory to append to")
	problem := fs.String("problem", "", "write a drill for this real problem, by number or part of its title")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := drill.UserDir()
	if err != nil {
		return err
	}
	deck, err := loadDeck(false)
	if err != nil {
		return err
	}
	var pre problemPrefill
	if *problem != "" {
		if pre, err = prefillFor(deck, openStore(), *problem); err != nil {
			return err
		}
	}
	path := filepath.Join(dir, filepath.Base(*file))
	if filepath.Ext(path) != ".json" {
		path += ".json"
	}
	err = addDrill(os.Stdin, os.Stdout, deck, path, pre)
	if errors.Is(err, errCancelled) {
		fmt.Println("nothing written.")
		return nil
	}
	return err
}

// addDrill interviews for one drill and appends it to path. The deck is only
// read from, to keep the new id unique against everything already loaded.
func addDrill(in io.Reader, out io.Writer, deck *drill.Set, path string, pre problemPrefill) error {
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	a := &asker{sc: sc, out: out}

	fmt.Fprintf(out, "new drill, appending to %s\n(blank line accepts the [default]; ctrl-d cancels)\n", path)
	if pre.Ref != "" {
		fmt.Fprintf(out, "for %s%s\n", pre.Ref, pre.covers())
	}
	fmt.Fprintln(out)

	d := drill.Drill{}
	kind, err := a.choose("kind", []string{"recall", "choice", "complexity", "snippet"}, "recall")
	if err != nil {
		return err
	}
	d.Kind = drill.Kind(kind)
	if d.Title, err = a.requiredOr("title", pre.Title); err != nil {
		return err
	}
	if d.Topic, err = a.topic(deck, pre.Topic); err != nil {
		return err
	}
	diff, err := a.choose("difficulty", []string{"easy", "medium"}, "medium")
	if err != nil {
		return err
	}
	d.Difficulty = drill.Difficulty(diff)
	if d.Prompt, err = a.required("prompt"); err != nil {
		return err
	}
	if d.Kind == drill.KindChoice {
		if d.Choices, d.Answer, err = a.choices(); err != nil {
			return err
		}
	} else if d.Answer, err = a.required("answer"); err != nil {
		return err
	}
	if d.Explanation, err = a.required("explanation"); err != nil {
		return err
	}
	if d.EstMinutes, err = a.minutes(); err != nil {
		return err
	}
	if d.Hints, err = a.list("hint", 3); err != nil {
		return err
	}
	if pre.Ref == "" {
		if d.Refs, err = a.list("ref (e.g. LC 739)", 5); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(out, "ref 1: %s\n", pre.Ref)
		extra, err := a.list("extra ref", 4)
		if err != nil {
			return err
		}
		d.Refs = append([]string{pre.Ref}, extra...)
	}
	d.ID = uniqueID(slugify(d.Title), deck)
	if d.ID, err = a.withDefault("id", d.ID); err != nil {
		return err
	}
	if _, taken := deck.ByID(d.ID); taken {
		fmt.Fprintf(out, "\nnote: id %q already exists, so this drill will replace it.\n", d.ID)
	}
	if err := d.Validate(); err != nil {
		return err
	}

	fmt.Fprintln(out)
	writeReview(out, d, progress.Record{}, false)
	ok, err := a.confirm(fmt.Sprintf("write to %s?", path))
	if err != nil || !ok {
		if err == nil {
			err = errCancelled
		}
		return err
	}
	if err := appendDrill(path, d); err != nil {
		return err
	}
	fmt.Fprintf(out, "added %s to %s\n", d.ID, path)
	if n := problemNumber(pre.Ref); n != "" {
		fmt.Fprintf(out, "try it: lcprac drill -problem %s\n", n)
	} else {
		fmt.Fprintf(out, "try it: lcprac drill -topic %s\n", d.Topic)
	}
	return nil
}

// problemPrefill is what -problem contributes to the interview: the ref the
// new drill carries, plus defaults read off the ref and the drills already
// behind it.
type problemPrefill struct {
	Ref     string
	Topic   string
	Title   string
	Covered bool
}

func (p problemPrefill) covers() string {
	if p.Covered {
		return ""
	}
	return " (nothing in the deck covers it yet)"
}

// prefillFor resolves a -problem query against the deck's refs and your
// attempt log, so a gap you logged with `attempt -new` can be turned into a
// drill by number. Nothing matching is not an error, unlike drill -problem:
// covering a problem the deck has never heard of is the point.
func prefillFor(set *drill.Set, store *progress.Store, query string) (problemPrefill, error) {
	// matchesProblem drops a leading "lc", so a bare "lc" would match nothing
	// and then be normalised into a ref named "lc".
	if strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(query)), "lc")) == "" {
		return problemPrefill{}, fmt.Errorf("name the problem, e.g. `lcprac add -problem 261`")
	}
	seen := map[string]bool{}
	var refs []string
	collect := func(ref string) {
		ref = strings.TrimSpace(ref)
		if ref == "" || seen[ref] || !matchesProblem(ref, query) {
			return
		}
		seen[ref] = true
		refs = append(refs, ref)
	}
	for _, d := range set.All() {
		for _, ref := range d.Refs {
			collect(ref)
		}
	}
	for _, a := range store.Attempts() {
		collect(a.Ref)
	}
	sort.Strings(refs)
	if len(refs) > 1 {
		return problemPrefill{}, fmt.Errorf("%q matches %d problems (%s); be more specific", query, len(refs), strings.Join(refs, "; "))
	}
	pre := problemPrefill{Ref: normalizeRef(query)}
	if len(refs) == 1 {
		pre.Ref = refs[0]
	}
	if pre.Ref == "" {
		return problemPrefill{}, fmt.Errorf("name the problem, e.g. `lcprac add -problem 261`")
	}
	pre.Title = refTitle(pre.Ref)
	pre.Topic, pre.Covered = refTopic(set, pre.Ref)
	return pre, nil
}

// refTitle drops the "LC <n>" head so the title prompt can default to the
// problem's own name.
func refTitle(ref string) string {
	fields := strings.Fields(ref)
	if len(fields) > 2 && strings.EqualFold(fields[0], "lc") {
		if _, err := strconv.Atoi(fields[1]); err == nil {
			return strings.Join(fields[2:], " ")
		}
	}
	return ref
}

// refTopic names the topic most of the problem's existing drills sit in, and
// reports whether the deck covers the problem at all.
func refTopic(set *drill.Set, ref string) (string, bool) {
	counts := map[string]int{}
	for _, d := range set.All() {
		for _, r := range d.Refs {
			if strings.EqualFold(strings.TrimSpace(r), ref) {
				counts[d.Topic]++
			}
		}
	}
	best, n := "", 0
	for t, c := range counts {
		if c > n || (c == n && t < best) {
			best, n = t, c
		}
	}
	return best, best != ""
}

// appendDrill adds d to the JSON array at path, creating the file if needed.
// The write goes through a temp file so a failure cannot truncate your drills.
func appendDrill(path string, d drill.Drill) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	var existing []drill.Drill
	switch raw, err := os.ReadFile(path); {
	case err == nil:
		if err := json.Unmarshal(raw, &existing); err != nil {
			return fmt.Errorf("%s: %w\n  fix the file by hand, or -file a different one", path, err)
		}
	case !os.IsNotExist(err):
		return err
	}
	existing = append(existing, d)
	body, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(body, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// slugify turns a title into an id: lowercase words joined by dashes, prefixed
// so your drills never look like builtin ones in a listing.
func slugify(title string) string {
	var b strings.Builder
	dash := true // leading dashes are dropped
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			dash = false
		case !dash:
			b.WriteByte('-')
			dash = true
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "drill"
	}
	if words := strings.Split(slug, "-"); len(words) > 6 {
		slug = strings.Join(words[:6], "-")
	}
	return "mine-" + slug
}

// uniqueID suffixes the slug until it names nothing in the deck.
func uniqueID(id string, deck *drill.Set) string {
	if _, taken := deck.ByID(id); !taken {
		return id
	}
	for n := 2; ; n++ {
		try := fmt.Sprintf("%s-%d", id, n)
		if _, taken := deck.ByID(try); !taken {
			return try
		}
	}
}

// asker reads one answer per line, re-prompting rather than failing on bad
// input so a typo never costs you the whole interview.
type asker struct {
	sc  *bufio.Scanner
	out io.Writer
}

func (a *asker) line(prompt string) (string, error) {
	fmt.Fprint(a.out, prompt)
	if !a.sc.Scan() {
		fmt.Fprintln(a.out)
		return "", errCancelled
	}
	return strings.TrimSpace(a.sc.Text()), nil
}

// requiredOr takes a default when one is known, so -problem can fill the
// title in without losing the "cannot be empty" rule when it cannot.
func (a *asker) requiredOr(label, def string) (string, error) {
	if def == "" {
		return a.required(label)
	}
	return a.withDefault(label, def)
}

func (a *asker) required(label string) (string, error) {
	for {
		s, err := a.line(label + ": ")
		if err != nil || s != "" {
			return s, err
		}
		fmt.Fprintf(a.out, "  %s cannot be empty.\n", label)
	}
}

func (a *asker) withDefault(label, def string) (string, error) {
	s, err := a.line(fmt.Sprintf("%s [%s]: ", label, def))
	if err != nil || s == "" {
		return def, err
	}
	return s, nil
}

func (a *asker) choose(label string, options []string, def string) (string, error) {
	for {
		s, err := a.withDefault(fmt.Sprintf("%s (%s)", label, strings.Join(options, "/")), def)
		if err != nil {
			return "", err
		}
		for _, o := range options {
			if strings.EqualFold(s, o) {
				return o, nil
			}
		}
		fmt.Fprintf(a.out, "  pick one of: %s\n", strings.Join(options, ", "))
	}
}

// topic offers the deck's existing topics so drills keep clustering into the
// same buckets instead of splintering into near-duplicates.
func (a *asker) topic(deck *drill.Set, def string) (string, error) {
	fmt.Fprintf(a.out, "existing topics: %s\n", strings.Join(deck.Topics(), ", "))
	return a.requiredOr("topic", def)
}

func (a *asker) minutes() (int, error) {
	for {
		s, err := a.withDefault("est minutes", "2")
		if err != nil {
			return 0, err
		}
		n, convErr := strconv.Atoi(s)
		if convErr == nil && n >= 1 && n <= 10 {
			return n, nil
		}
		fmt.Fprintln(a.out, "  give a whole number of minutes, 1 to 10.")
	}
}

// choices collects the options, then takes the answer by number so it always
// matches one of them exactly.
func (a *asker) choices() ([]string, string, error) {
	var opts []string
	for {
		s, err := a.line(fmt.Sprintf("choice %d (blank when done): ", len(opts)+1))
		if err != nil {
			return nil, "", err
		}
		if s == "" {
			if len(opts) >= 2 {
				break
			}
			fmt.Fprintln(a.out, "  a choice drill needs at least 2 choices.")
			continue
		}
		opts = append(opts, s)
	}
	for {
		s, err := a.line(fmt.Sprintf("which is correct (1-%d): ", len(opts)))
		if err != nil {
			return nil, "", err
		}
		if n, convErr := strconv.Atoi(s); convErr == nil && n >= 1 && n <= len(opts) {
			return opts, opts[n-1], nil
		}
		fmt.Fprintf(a.out, "  give a number from 1 to %d.\n", len(opts))
	}
}

// list collects up to max optional entries, blank line ends it.
func (a *asker) list(label string, max int) ([]string, error) {
	var got []string
	for len(got) < max {
		s, err := a.line(fmt.Sprintf("%s %d (blank to skip): ", label, len(got)+1))
		if err != nil {
			return nil, err
		}
		if s == "" {
			break
		}
		got = append(got, s)
	}
	return got, nil
}

func (a *asker) confirm(question string) (bool, error) {
	s, err := a.withDefault(question, "y")
	if err != nil {
		return false, err
	}
	return strings.EqualFold(s, "y") || strings.EqualFold(s, "yes"), nil
}
