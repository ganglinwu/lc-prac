package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
)

// exampleFile is what -init writes: two valid drills, so the file both
// documents the schema and starts working the moment it lands.
const exampleFile = `[
  {
    "id": "mine-monotonic-stack-invariant",
    "title": "Monotonic stack: what the stack holds",
    "kind": "recall",
    "topic": "stack",
    "difficulty": "medium",
    "est_minutes": 2,
    "prompt": "For next-greater-element, what invariant does the stack keep, and what do you pop on?",
    "answer": "The stack holds indices whose next greater element is still unknown, kept decreasing by value. On each new value, pop every index whose value is smaller: the new value is their answer.",
    "explanation": "Each index is pushed and popped at most once, so the scan is O(n) despite the inner while loop.",
    "hints": ["What question is each stacked index still waiting to have answered?"],
    "refs": ["LC 496", "LC 739"]
  },
  {
    "id": "mine-sliding-window-cost",
    "title": "Cost of a variable-size sliding window",
    "kind": "complexity",
    "topic": "sliding-window",
    "difficulty": "easy",
    "est_minutes": 1,
    "prompt": "Right pointer scans once, left pointer only ever moves forward. Time?",
    "answer": "O(n)",
    "explanation": "Two pointers that each advance at most n times give 2n moves, not n^2, even though the shrink sits inside the grow loop."
  }
]
`

func cmdMine(args []string) error {
	fs := flag.NewFlagSet("mine", flag.ContinueOnError)
	init := fs.Bool("init", false, "write an example drill file if the directory has none")
	rm := fs.String("rm", "", "delete one of your drills by id, on every machine")
	if err := fs.Parse(args); err != nil {
		return err
	}
	dir, err := drill.UserDir()
	if err != nil {
		return err
	}
	if *init {
		if err := writeExample(dir); err != nil {
			return err
		}
	}
	if *rm != "" {
		return removeMine(os.Stdout, dir, *rm)
	}

	mine, err := drill.LoadUserDir(dir)
	fmt.Printf("your drills: %s\n", dir)
	if err != nil {
		return err
	}
	if len(mine) == 0 {
		fmt.Println("nothing there yet. `lcprac mine -init` writes an example to copy.")
		return nil
	}

	builtin, err := drill.Builtin()
	if err != nil {
		return err
	}
	sort.Slice(mine, func(i, j int) bool { return mine[i].ID < mine[j].ID })
	added, replaced := 0, 0
	for _, d := range mine {
		note := "new"
		if _, ok := builtin.ByID(d.ID); ok {
			note = "replaces builtin"
			replaced++
		} else {
			added++
		}
		fmt.Printf("  %-28s %-16s %-11s %dm  %s (%s)\n", d.ID, d.Topic, d.Kind, d.EstMinutes, d.Title, note)
	}
	if _, err := drill.Merge(builtin, mine); err != nil {
		return err
	}
	fmt.Printf("\n%d added, %d replacing a builtin drill\n", added, replaced)
	return nil
}

// removeMine turns one of your drills into a tombstone. The entry stays in
// its file with only its id, because a merge that only unions cannot learn
// about a row that simply vanished: the next sync would hand it back.
func removeMine(w io.Writer, dir, id string) error {
	files, found, err := drill.DeleteDrill(dir, id, time.Now())
	if err != nil {
		return err
	}
	if !found {
		all, err := drill.LoadUserDirAll(dir)
		if err != nil {
			return err
		}
		for _, d := range all {
			if d.ID == id {
				fmt.Fprintf(w, "%s is already deleted.\n", id)
				return nil
			}
		}
		return fmt.Errorf("no drill of yours with id %q (builtin drills cannot be deleted)", id)
	}
	fmt.Fprintf(w, "deleted %s (%d file(s) rewritten).\n", id, files)
	fmt.Fprintln(w, "run `lcprac sync` to remove it from your other machines.")
	return nil
}

// writeExample seeds the directory, refusing to clobber an existing file.
func writeExample(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(dir, "example.json")
	if _, err := os.Stat(path); err == nil {
		fmt.Printf("already there: %s\n", path)
		return nil
	}
	if err := os.WriteFile(path, []byte(exampleFile), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", path)
	return nil
}
