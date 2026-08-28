package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

func paceDeck(t *testing.T) *drill.Set {
	t.Helper()
	set, err := drill.NewSet([]drill.Drill{
		{ID: "a", Title: "two sum", Kind: drill.KindRecall, Topic: "hashmap", Difficulty: drill.Easy, EstMinutes: 2, Prompt: "p", Answer: "a", Explanation: "e"},
		{ID: "b", Title: "binary search bounds", Kind: drill.KindRecall, Topic: "binary-search", Difficulty: drill.Easy, EstMinutes: 4, Prompt: "p", Answer: "a", Explanation: "e"},
		{ID: "c", Title: "untimed", Kind: drill.KindRecall, Topic: "stack", Difficulty: drill.Easy, EstMinutes: 3, Prompt: "p", Answer: "a", Explanation: "e"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestPaceRowsOrdersByOverrun(t *testing.T) {
	set := paceDeck(t)
	store := progress.New("")
	now := time.Now()
	store.Record("a", true, now)
	store.AddTime("a", 6*time.Minute) // 2m est, 200% over
	store.Record("b", true, now)
	store.AddTime("b", 5*time.Minute) // 4m est, 25% over
	store.Record("d-gone", true, now) // no such drill any more
	store.AddTime("d-gone", time.Hour)

	rows := paceRows(set, store, "")
	if len(rows) != 2 {
		t.Fatalf("want 2 rows (untimed and missing drills dropped), got %d", len(rows))
	}
	if rows[0].Drill.ID != "a" || rows[1].Drill.ID != "b" {
		t.Fatalf("want slowest-against-estimate first, got %s then %s", rows[0].Drill.ID, rows[1].Drill.ID)
	}
	if rows[0].Over() != 200 {
		t.Errorf("want 200%% over, got %d", rows[0].Over())
	}
	if rows[0].Avg != 6*time.Minute {
		t.Errorf("want 6m average, got %s", rows[0].Avg)
	}
}

func TestPaceRowsAveragesAcrossAttempts(t *testing.T) {
	set := paceDeck(t)
	store := progress.New("")
	now := time.Now()
	store.Record("a", true, now)
	store.AddTime("a", 1*time.Minute)
	store.Record("a", false, now)
	store.AddTime("a", 3*time.Minute)

	rows := paceRows(set, store, "")
	if len(rows) != 1 || rows[0].Avg != 2*time.Minute {
		t.Fatalf("want one row averaging 2m, got %+v", rows)
	}
	if rows[0].Seen != 2 {
		t.Errorf("want 2 attempts, got %d", rows[0].Seen)
	}
}

func TestPaceRowsFiltersTopic(t *testing.T) {
	set := paceDeck(t)
	store := progress.New("")
	now := time.Now()
	for _, id := range []string{"a", "b"} {
		store.Record(id, true, now)
		store.AddTime(id, time.Minute)
	}
	rows := paceRows(set, store, "binary-search")
	if len(rows) != 1 || rows[0].Drill.ID != "b" {
		t.Fatalf("want only the binary-search row, got %+v", rows)
	}
}

func TestWritePaceLimitsAndSummarises(t *testing.T) {
	set := paceDeck(t)
	store := progress.New("")
	now := time.Now()
	store.Record("a", true, now)
	store.AddTime("a", 6*time.Minute)
	store.Record("b", true, now)
	store.AddTime("b", 2*time.Minute)

	var buf bytes.Buffer
	writePace(&buf, paceRows(set, store, ""), 1)
	out := buf.String()
	if !strings.Contains(out, "two sum") {
		t.Errorf("want the slowest drill shown, got:\n%s", out)
	}
	if strings.Contains(out, "binary search bounds") {
		t.Errorf("-n 1 should have hidden the second row, got:\n%s", out)
	}
	if !strings.Contains(out, "200% over") {
		t.Errorf("want the overrun spelled out, got:\n%s", out)
	}
	// Both rows still count towards the totals even though one was not shown.
	if !strings.Contains(out, "2 timed drill(s): 4m00s each") {
		t.Errorf("want totals over every timed drill, got:\n%s", out)
	}
	if !strings.Contains(out, "fits about 3 of them") {
		t.Errorf("want the sitting estimate, got:\n%s", out)
	}
}

func TestWritePaceMarksOnPace(t *testing.T) {
	set := paceDeck(t)
	store := progress.New("")
	store.Record("b", true, time.Now())
	store.AddTime("b", 90*time.Second)

	var buf bytes.Buffer
	writePace(&buf, paceRows(set, store, ""), 0)
	if out := buf.String(); !strings.Contains(out, "on pace") {
		t.Errorf("want an under-estimate drill marked on pace, got:\n%s", out)
	}
}

func TestShortDuration(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{45 * time.Second, "45s"},
		{0, "0s"},
		{130 * time.Second, "2m10s"},
		{2 * time.Minute, "2m00s"},
	}
	for _, c := range cases {
		if got := short(c.d); got != c.want {
			t.Errorf("short(%s) = %q, want %q", c.d, got, c.want)
		}
	}
}
