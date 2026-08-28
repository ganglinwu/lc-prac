// Package session assembles a short practice run out of individual drills.
package session

import (
	"fmt"
	"math/rand/v2"
	"sort"

	"github.com/ganglinwu/lc-prac/internal/drill"
)

// Options constrain which drills a session may draw from and how long it runs.
type Options struct {
	// BudgetMinutes is the target length of the session. Drills are added
	// while they still fit.
	BudgetMinutes int
	// Topic, Kind and Difficulty are optional filters; zero means "any".
	Topic      string
	Kind       drill.Kind
	Difficulty drill.Difficulty
	// Seed makes selection reproducible. Callers that want variety pass a
	// changing value (typically the wall clock).
	Seed uint64
}

// DefaultBudget is the session length the CLI aims for when none is given.
const DefaultBudget = 12

// Session is an ordered list of drills that fits inside a time budget.
type Session struct {
	Drills        []drill.Drill
	BudgetMinutes int
}

// TotalMinutes is the summed estimate of the chosen drills.
func (s Session) TotalMinutes() int {
	total := 0
	for _, d := range s.Drills {
		total += d.EstMinutes
	}
	return total
}

// Build picks drills that fit the budget, rotating through topics so a short
// session touches several patterns instead of drilling one to death.
func Build(set *drill.Set, opts Options) (Session, error) {
	if opts.BudgetMinutes <= 0 {
		opts.BudgetMinutes = DefaultBudget
	}
	candidates := set.Filter(opts.Topic, opts.Kind, opts.Difficulty)
	if len(candidates) == 0 {
		return Session{}, fmt.Errorf("no drills match topic=%q kind=%q difficulty=%q", opts.Topic, opts.Kind, opts.Difficulty)
	}

	rng := rand.New(rand.NewPCG(opts.Seed, opts.Seed^0x9e3779b97f4a7c15))
	buckets := bucketByTopic(candidates, rng)

	chosen := make([]drill.Drill, 0, len(candidates))
	remaining := opts.BudgetMinutes
	for progress := true; progress; {
		progress = false
		for i := range buckets {
			b := &buckets[i]
			for len(b.drills) > 0 {
				d := b.drills[0]
				b.drills = b.drills[1:]
				if d.EstMinutes <= remaining {
					chosen = append(chosen, d)
					remaining -= d.EstMinutes
					progress = true
					break
				}
			}
		}
	}
	if len(chosen) == 0 {
		return Session{}, fmt.Errorf("budget of %d minutes is too small for any matching drill", opts.BudgetMinutes)
	}
	return Session{Drills: chosen, BudgetMinutes: opts.BudgetMinutes}, nil
}

type bucket struct {
	topic  string
	drills []drill.Drill
}

// bucketByTopic groups drills by topic and shuffles both the topic order and
// the drills inside each topic.
func bucketByTopic(drills []drill.Drill, rng *rand.Rand) []bucket {
	byTopic := map[string][]drill.Drill{}
	for _, d := range drills {
		byTopic[d.Topic] = append(byTopic[d.Topic], d)
	}
	topics := make([]string, 0, len(byTopic))
	for t := range byTopic {
		topics = append(topics, t)
	}
	sort.Strings(topics) // sort first so the shuffle is the only source of order
	rng.Shuffle(len(topics), func(i, j int) { topics[i], topics[j] = topics[j], topics[i] })

	out := make([]bucket, 0, len(topics))
	for _, t := range topics {
		ds := byTopic[t]
		rng.Shuffle(len(ds), func(i, j int) { ds[i], ds[j] = ds[j], ds[i] })
		out = append(out, bucket{topic: t, drills: ds})
	}
	return out
}
