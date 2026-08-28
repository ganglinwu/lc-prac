package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

func problemFixture(t *testing.T) (*drill.Set, *progress.Store) {
	t.Helper()
	set, err := drill.NewSet([]drill.Drill{
		{ID: "a", Title: "A", Kind: drill.KindRecall, Topic: "stack", Difficulty: drill.Easy,
			EstMinutes: 2, Prompt: "p", Answer: "x", Explanation: "e", Refs: []string{"LC 20 Valid Parentheses"}},
		{ID: "b", Title: "B", Kind: drill.KindRecall, Topic: "stack", Difficulty: drill.Easy,
			EstMinutes: 2, Prompt: "p", Answer: "x", Explanation: "e", Refs: []string{"LC 20 Valid Parentheses", "LC 739 Daily Temperatures"}},
		{ID: "c", Title: "C", Kind: drill.KindRecall, Topic: "dp", Difficulty: drill.Easy,
			EstMinutes: 2, Prompt: "p", Answer: "x", Explanation: "e", Refs: []string{"LC 70 Climbing Stairs"}},
		{ID: "d", Title: "D", Kind: drill.KindRecall, Topic: "dp", Difficulty: drill.Easy,
			EstMinutes: 2, Prompt: "p", Answer: "x", Explanation: "e"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return set, progress.New("")
}

func TestProblemRowsFoldRefsAcrossDrills(t *testing.T) {
	set, store := problemFixture(t)
	rows := problemRows(set, store, "", time.Now())
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3 (a ref-less drill contributes nothing)", len(rows))
	}
	var parens problemRow
	for _, r := range rows {
		if strings.HasPrefix(r.Ref, "LC 20") {
			parens = r
		}
	}
	if parens.Drills != 2 {
		t.Errorf("LC 20 backed by %d drills, want 2", parens.Drills)
	}
	if got := strings.Join(parens.Topics, ","); got != "stack" {
		t.Errorf("topics = %q, want stack", got)
	}
	for _, r := range rows {
		if r.tier() != 1 {
			t.Errorf("%s is tier %d with no history, want 1 (untried)", r.Ref, r.tier())
		}
	}
}

func TestProblemRowsRankWeakThenUntriedThenSolid(t *testing.T) {
	set, store := problemFixture(t)
	now := time.Now()
	store.Record("a", false, now) // LC 20 shaky at 50%
	store.Record("a", true, now)
	store.Record("c", true, now) // LC 70 solid

	rows := problemRows(set, store, "", time.Now())
	if rows[0].Ref != "LC 20 Valid Parentheses" {
		t.Fatalf("first = %s, want the shaky problem", rows[0].Ref)
	}
	if rows[0].Accuracy() != 50 || rows[0].Tried != 1 || rows[0].Seen != 2 {
		t.Errorf("LC 20 = %d%% over %d attempts on %d drill(s) tried", rows[0].Accuracy(), rows[0].Seen, rows[0].Tried)
	}
	if rows[1].Ref != "LC 739 Daily Temperatures" {
		t.Errorf("second = %s, want the untried problem", rows[1].Ref)
	}
	if last := rows[len(rows)-1]; last.Ref != "LC 70 Climbing Stairs" || last.tier() != 2 {
		t.Errorf("last = %s (tier %d), want the solid problem", last.Ref, last.tier())
	}
}

func TestProblemRowsPutMostBackedTopicFirst(t *testing.T) {
	set, err := drill.NewSet([]drill.Drill{
		{ID: "a", Title: "A", Kind: drill.KindRecall, Topic: "complexity", Difficulty: drill.Easy,
			EstMinutes: 2, Prompt: "p", Answer: "x", Explanation: "e", Refs: []string{"LC 56 Merge Intervals"}},
		{ID: "b", Title: "B", Kind: drill.KindRecall, Topic: "intervals", Difficulty: drill.Easy,
			EstMinutes: 2, Prompt: "p", Answer: "x", Explanation: "e", Refs: []string{"LC 56 Merge Intervals"}},
		{ID: "c", Title: "C", Kind: drill.KindRecall, Topic: "intervals", Difficulty: drill.Easy,
			EstMinutes: 2, Prompt: "p", Answer: "x", Explanation: "e", Refs: []string{"LC 56 Merge Intervals"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rows := problemRows(set, progress.New(""), "", time.Now())
	if got := strings.Join(rows[0].Topics, ", "); got != "intervals, complexity" {
		t.Errorf("topics = %q, want the two-drill topic first", got)
	}
}

func TestProblemRowsFilterByTopic(t *testing.T) {
	set, store := problemFixture(t)
	rows := problemRows(set, store, "dp", time.Now())
	if len(rows) != 1 || rows[0].Ref != "LC 70 Climbing Stairs" {
		t.Fatalf("dp rows = %+v, want only LC 70", rows)
	}
	if got := problemRows(set, store, "graphs", time.Now()); len(got) != 0 {
		t.Errorf("unknown topic gave %d rows", len(got))
	}
}

func TestWriteProblemsTruncatesButTalliesEverything(t *testing.T) {
	set, store := problemFixture(t)
	store.Record("a", false, time.Now())
	rows := problemRows(set, store, "", time.Now())

	var sb strings.Builder
	writeProblems(&sb, rows, 1)
	out := sb.String()
	if strings.Count(out, "LC ") < 2 { // one row, plus the next-up line
		t.Errorf("-n 1 printed no row:\n%s", out)
	}
	if strings.Contains(out, "LC 70") {
		t.Errorf("-n 1 showed a second row:\n%s", out)
	}
	if !strings.Contains(out, "3 problem(s) behind the deck: 1 shaky, 2 not drilled yet, 0 solid.") {
		t.Errorf("tally does not span every row:\n%s", out)
	}
	if !strings.Contains(out, "next up: attempt LC 20 Valid Parentheses for real") {
		t.Errorf("missing next-up pointer:\n%s", out)
	}
}

func TestBuiltinDeckNamesProblems(t *testing.T) {
	set, err := drill.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	rows := problemRows(set, progress.New(""), "", time.Now())
	if len(rows) < 20 {
		t.Errorf("builtin deck names only %d problems, want at least 20", len(rows))
	}
	for _, r := range rows {
		if len(r.Topics) == 0 {
			t.Errorf("%s has no topic", r.Ref)
		}
	}
}
