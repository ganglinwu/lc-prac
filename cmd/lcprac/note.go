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

// cmdNote keeps your own words on a drill: the phrasing that finally made the
// pattern stick, which the canned explanation cannot give you.
func cmdNote(args []string) error {
	fs := flag.NewFlagSet("note", flag.ContinueOnError)
	clear := fs.Bool("clear", false, "remove the note on this drill")
	if err := fs.Parse(args); err != nil {
		return err
	}
	set, _, err := drill.Combined()
	if err != nil {
		return err
	}
	store := openStore()
	rest := fs.Args()
	// -clear reads naturally after the id, where flag.Parse stops looking.
	if n := len(rest); n > 0 && (rest[n-1] == "-clear" || rest[n-1] == "--clear") {
		*clear, rest = true, rest[:n-1]
	}
	if len(rest) == 0 {
		if *clear {
			return fmt.Errorf("which drill? `lcprac note -clear <id>`")
		}
		listNotes(os.Stdout, set, store)
		return nil
	}

	id := rest[0]
	d, ok := set.ByID(id)
	if !ok {
		return fmt.Errorf("no drill with id %q (try `lcprac list`)", id)
	}
	text := strings.TrimSpace(strings.Join(rest[1:], " "))
	switch {
	case *clear:
		if store.Note(id) == "" {
			fmt.Printf("%s has no note.\n", id)
			return nil
		}
		store.SetNote(id, "")
		if err := store.Save(); err != nil {
			return err
		}
		fmt.Printf("note cleared on %s.\n", id)
		return nil
	case text == "":
		if n := store.Note(id); n != "" {
			fmt.Printf("%s  %s\n%s\n", id, d.Title, indent(n))
		} else {
			fmt.Printf("%s  %s\nno note yet: `lcprac note %s <your words>`\n", id, d.Title, id)
		}
		return nil
	}
	store.SetNote(id, text)
	if err := store.Save(); err != nil {
		return err
	}
	fmt.Printf("noted on %s. it comes back with the drill.\n", id)
	return nil
}

// listNotes prints every note you have written, one drill per block, so the
// set of them reads as your own crib sheet.
func listNotes(w io.Writer, set *drill.Set, store *progress.Store) {
	noted := store.Noted()
	if len(noted) == 0 {
		fmt.Fprintln(w, "no notes yet: `lcprac note <id> <your words>` writes one.")
		return
	}
	for _, r := range noted {
		title := ""
		if d, ok := set.ByID(r.DrillID); ok {
			title = "  " + d.Title
		}
		fmt.Fprintf(w, "%s%s\n%s\n\n", r.DrillID, title, indent(r.Note))
	}
	fmt.Fprintf(w, "%d note(s).\n", len(noted))
}
