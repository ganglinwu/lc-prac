package main

import (
	"strings"
	"testing"
	"time"

	"github.com/ganglinwu/lc-prac/internal/drill"
	"github.com/ganglinwu/lc-prac/internal/progress"
)

func reviewFixture(t *testing.T) (*drill.Set, *progress.Store) {
	t.Helper()
	set, err := drill.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	return set, progress.New("")
}

// miss records n wrong answers on a drill, which is what makes a leech.
func miss(s *progress.Store, id string, n int) {
	now := time.Now()
	for i := 0; i < n; i++ {
		s.Record(id, false, now)
	}
}

func TestPickReviewTakesWorstLeechesFirst(t *testing.T) {
	set, store := reviewFixture(t)
	ids := set.Filter("", "", "")
	if len(ids) < 3 {
		t.Fatal("deck too small for this test")
	}
	miss(store, ids[0].ID, 3)
	miss(store, ids[1].ID, 5)
	miss(store, ids[2].ID, 1)

	got := pickReview(set, store, "", 0, false)
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2 (one miss is not a leech)", len(got))
	}
	if got[0].drill.ID != ids[1].ID {
		t.Errorf("first = %s, want the 5-miss drill %s", got[0].drill.ID, ids[1].ID)
	}
	if n := pickReview(set, store, "", 1, false); len(n) != 1 {
		t.Errorf("-n 1 gave %d items", len(n))
	}
	if all := pickReview(set, store, "", 0, true); len(all) != 3 {
		t.Errorf("-all gave %d items, want 3", len(all))
	}
}

func TestPickReviewFiltersTopicAndDropsMissingDrills(t *testing.T) {
	set, store := reviewFixture(t)
	d := set.Filter("", "", "")[0]
	miss(store, d.ID, 3)
	miss(store, "deleted-user-drill", 3)

	if got := pickReview(set, store, d.Topic, 0, false); len(got) != 1 {
		t.Errorf("topic %s gave %d items, want 1", d.Topic, len(got))
	}
	other := ""
	for _, tp := range set.Topics() {
		if tp != d.Topic {
			other = tp
			break
		}
	}
	if got := pickReview(set, store, other, 0, false); len(got) != 0 {
		t.Errorf("topic %s gave %d items, want 0", other, len(got))
	}
}

func TestWriteReviewShowsTheAnswer(t *testing.T) {
	set, _ := reviewFixture(t)
	d := set.Filter("", drill.KindChoice, "")[0]
	var b strings.Builder
	writeReview(&b, d, progress.Record{Seen: 4, Correct: 1}, false)
	out := b.String()
	for _, want := range []string{d.ID, d.Title, d.Answer, d.Explanation, "missed 3 of 4", "1. " + d.Choices[0]} {
		if !strings.Contains(out, want) {
			t.Errorf("review output missing %q:\n%s", want, out)
		}
	}
}
