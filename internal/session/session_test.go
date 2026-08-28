package session

import (
	"testing"

	"github.com/ganglinwu/lc-prac/internal/drill"
)

func mk(id, topic string, mins int) drill.Drill {
	return drill.Drill{
		ID: id, Title: id, Kind: drill.KindRecall, Topic: topic,
		Difficulty: drill.Easy, EstMinutes: mins, Prompt: "p", Answer: "a", Explanation: "e",
	}
}

func setOf(t *testing.T, ds ...drill.Drill) *drill.Set {
	t.Helper()
	s, err := drill.NewSet(ds)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBuildRespectsBudget(t *testing.T) {
	set := setOf(t, mk("a", "x", 4), mk("b", "y", 4), mk("c", "z", 4), mk("d", "w", 4))
	s, err := Build(set, Options{BudgetMinutes: 10, Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if s.TotalMinutes() > 10 {
		t.Fatalf("total %d exceeds budget", s.TotalMinutes())
	}
	if len(s.Drills) != 2 {
		t.Fatalf("expected 2 drills to fit in 10m, got %d", len(s.Drills))
	}
}

func TestBuildFillsBudgetWithSmallerDrills(t *testing.T) {
	// A 5m drill cannot fit in the trailing 2m, so the builder must keep
	// looking rather than stopping at the first drill that does not fit.
	set := setOf(t, mk("big", "x", 5), mk("small", "x", 2))
	s, err := Build(set, Options{BudgetMinutes: 7, Seed: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Drills) != 2 {
		t.Fatalf("expected both drills to fit exactly, got %d", len(s.Drills))
	}
}

func TestBuildPrefersTopicVariety(t *testing.T) {
	set := setOf(t,
		mk("a1", "alpha", 2), mk("a2", "alpha", 2), mk("a3", "alpha", 2),
		mk("b1", "beta", 2), mk("c1", "gamma", 2),
	)
	s, err := Build(set, Options{BudgetMinutes: 6, Seed: 7})
	if err != nil {
		t.Fatal(err)
	}
	topics := map[string]bool{}
	for _, d := range s.Drills {
		topics[d.Topic] = true
	}
	if len(topics) != 3 {
		t.Fatalf("expected 3 distinct topics in 3 drills, got %v", topics)
	}
}

func TestBuildIsDeterministicForASeed(t *testing.T) {
	set := setOf(t, mk("a", "x", 2), mk("b", "y", 2), mk("c", "z", 2), mk("d", "w", 2))
	first, err := Build(set, Options{BudgetMinutes: 6, Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	second, err := Build(set, Options{BudgetMinutes: 6, Seed: 42})
	if err != nil {
		t.Fatal(err)
	}
	for i := range first.Drills {
		if first.Drills[i].ID != second.Drills[i].ID {
			t.Fatalf("same seed produced different sessions: %v vs %v", first.Drills, second.Drills)
		}
	}
}

func TestBuildNoDuplicateDrills(t *testing.T) {
	set := setOf(t, mk("a", "x", 1), mk("b", "y", 1))
	s, err := Build(set, Options{BudgetMinutes: 60, Seed: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Drills) != 2 {
		t.Fatalf("budget larger than the deck must not repeat drills, got %d", len(s.Drills))
	}
}

func TestBuildErrors(t *testing.T) {
	set := setOf(t, mk("a", "x", 4))
	if _, err := Build(set, Options{BudgetMinutes: 10, Topic: "nope"}); err == nil {
		t.Error("expected error when no drill matches the filter")
	}
	if _, err := Build(set, Options{BudgetMinutes: 1}); err == nil {
		t.Error("expected error when the budget fits nothing")
	}
}

func TestBuildDefaultsBudget(t *testing.T) {
	set := setOf(t, mk("a", "x", 3))
	s, err := Build(set, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if s.BudgetMinutes != DefaultBudget {
		t.Fatalf("BudgetMinutes = %d, want %d", s.BudgetMinutes, DefaultBudget)
	}
}

func TestBuildOnBuiltinDeckHitsTargetLength(t *testing.T) {
	set, err := drill.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	for _, budget := range []int{10, 12, 15} {
		s, err := Build(set, Options{BudgetMinutes: budget, Seed: uint64(budget)})
		if err != nil {
			t.Fatal(err)
		}
		if s.TotalMinutes() > budget {
			t.Errorf("budget %d: session is %dm", budget, s.TotalMinutes())
		}
		if s.TotalMinutes() < budget-1 {
			t.Errorf("budget %d: session only %dm, wasting the window", budget, s.TotalMinutes())
		}
	}
}

func TestBuildPrefersHighPriorityDrills(t *testing.T) {
	set := setOf(t,
		mk("cold-a", "x", 3), mk("hot-a", "x", 3),
		mk("cold-b", "y", 3), mk("hot-b", "y", 3),
	)
	priority := func(d drill.Drill) int {
		if d.ID[:3] == "hot" {
			return 200
		}
		return 1
	}
	// Every seed must surface the due drills first, not just a lucky one.
	for seed := uint64(1); seed <= 20; seed++ {
		s, err := Build(set, Options{BudgetMinutes: 6, Seed: seed, Priority: priority})
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range s.Drills {
			if d.ID[:3] != "hot" {
				t.Fatalf("seed %d picked %s over a due drill", seed, d.ID)
			}
		}
	}
}

func TestBuildStillFillsBudgetWhenPriorityRunsOut(t *testing.T) {
	set := setOf(t, mk("hot", "x", 3), mk("cold-a", "y", 3), mk("cold-b", "z", 3))
	priority := func(d drill.Drill) int {
		if d.ID == "hot" {
			return 200
		}
		return 1
	}
	s, err := Build(set, Options{BudgetMinutes: 9, Seed: 7, Priority: priority})
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Drills) != 3 {
		t.Fatalf("priority should reorder, not exclude: got %d drills", len(s.Drills))
	}
	if s.Drills[0].ID != "hot" {
		t.Fatalf("highest priority drill should come first, got %s", s.Drills[0].ID)
	}
}

func TestBuildIgnoresNilPriority(t *testing.T) {
	set := setOf(t, mk("a", "x", 3), mk("b", "y", 3))
	withNil, err := Build(set, Options{BudgetMinutes: 6, Seed: 3})
	if err != nil {
		t.Fatal(err)
	}
	flat, err := Build(set, Options{BudgetMinutes: 6, Seed: 3, Priority: func(drill.Drill) int { return 5 }})
	if err != nil {
		t.Fatal(err)
	}
	for i := range withNil.Drills {
		if withNil.Drills[i].ID != flat.Drills[i].ID {
			t.Fatalf("a flat priority changed the order: %v vs %v", withNil.Drills, flat.Drills)
		}
	}
}
