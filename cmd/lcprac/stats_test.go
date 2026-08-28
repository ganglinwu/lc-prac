package main

import (
	"testing"
	"time"
)

func TestFocusPicksWeakestTopicWithData(t *testing.T) {
	stats := []topicStat{
		{Topic: "dp", Tried: 3, Total: 4, Seen: 6, Correct: 2},
		{Topic: "graphs", Tried: 2, Total: 4, Seen: 4, Correct: 4},
		{Topic: "greedy", Tried: 1, Total: 5, Seen: 2, Correct: 0},
	}
	if got := focus(stats); got != "dp" {
		t.Errorf("focus = %q, want dp (greedy has too few attempts to judge)", got)
	}
}

func TestFocusFallsBackToUntried(t *testing.T) {
	stats := []topicStat{
		{Topic: "dp", Tried: 3, Total: 4, Seen: 2, Correct: 1},
		{Topic: "greedy", Tried: 0, Total: 5},
	}
	if got := focus(stats); got != "greedy" {
		t.Errorf("focus = %q, want greedy", got)
	}
	if got := focus(nil); got != "" {
		t.Errorf("focus(nil) = %q, want empty", got)
	}
}

func TestWholeMinutesRoundsUpFromZero(t *testing.T) {
	if got := wholeMinutes(20 * time.Second); got != 1 {
		t.Errorf("wholeMinutes(20s) = %d, want 1", got)
	}
	if got := wholeMinutes(11*time.Minute + 40*time.Second); got != 12 {
		t.Errorf("wholeMinutes(11m40s) = %d, want 12", got)
	}
}
