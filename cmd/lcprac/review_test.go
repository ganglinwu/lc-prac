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

	got := pickReview(set, store, "", 0, false, false)
	if len(got) != 2 {
		t.Fatalf("got %d items, want 2 (one miss is not a leech)", len(got))
	}
	if got[0].drill.ID != ids[1].ID {
		t.Errorf("first = %s, want the 5-miss drill %s", got[0].drill.ID, ids[1].ID)
	}
	if n := pickReview(set, store, "", 1, false, false); len(n) != 1 {
		t.Errorf("-n 1 gave %d items", len(n))
	}
	if all := pickReview(set, store, "", 0, true, false); len(all) != 3 {
		t.Errorf("-all gave %d items, want 3", len(all))
	}
}

func TestPickReviewFiltersTopicAndDropsMissingDrills(t *testing.T) {
	set, store := reviewFixture(t)
	d := set.Filter("", "", "")[0]
	miss(store, d.ID, 3)
	miss(store, "deleted-user-drill", 3)

	if got := pickReview(set, store, d.Topic, 0, false, false); len(got) != 1 {
		t.Errorf("topic %s gave %d items, want 1", d.Topic, len(got))
	}
	other := ""
	for _, tp := range set.Topics() {
		if tp != d.Topic {
			other = tp
			break
		}
	}
	if got := pickReview(set, store, other, 0, false, false); len(got) != 0 {
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

func TestPickReviewCodeShowsSavedSolutions(t *testing.T) {
	set, store := reviewFixture(t)
	code := set.Filter("", drill.KindCode, "")
	if len(code) < 1 {
		t.Fatal("deck has no code drills")
	}
	now := time.Now()
	other := set.Filter("", "", "")[0]
	store.Record(other.ID, false, now)
	store.Record(code[0].ID, true, now)
	store.SetSolution(code[0].ID, "func mine() {}", true, now)

	got := pickReview(set, store, "", 0, false, true)
	if len(got) != 1 || got[0].drill.ID != code[0].ID {
		t.Fatalf("pickReview(-code) = %+v, want only the solved code drill", got)
	}
}

func TestWriteReviewShowsYourSavedCode(t *testing.T) {
	set, store := reviewFixture(t)
	d := set.Filter("", "", "")[0]
	now := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	store.Record(d.ID, true, now)
	store.SetSolution(d.ID, "func mine() int { return 1 }", true, now)
	rec, _ := store.Get(d.ID)

	var b strings.Builder
	writeReview(&b, d, rec, false)
	out := b.String()
	if !strings.Contains(out, "your code (passed, 1 Mar 2026)") {
		t.Errorf("missing the saved-code heading, got:\n%s", out)
	}
	if !strings.Contains(out, "func mine() int { return 1 }") {
		t.Errorf("missing the saved source, got:\n%s", out)
	}

	var none strings.Builder
	writeReview(&none, d, progress.Record{}, false)
	if strings.Contains(none.String(), "your code") {
		t.Error("a drill with no saved code should not print the heading")
	}
}
