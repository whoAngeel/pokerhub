package engine

import (
	"testing"
	"time"
)

func TestEquityAAvsRandomPreflop(t *testing.T) {
	hole := cs("As", "Ah")
	got := Equity(hole, nil, 1, 20000, testRNG(5))
	want := 0.85
	if diff := got - want; diff < -0.03 || diff > 0.03 {
		t.Fatalf("AA vs 1 random hand equity = %.4f, want %.2f +/- 0.03", got, want)
	}
}

func TestEquityMadeNutsOnRiver(t *testing.T) {
	// Royal flush on the river beats anything else; equity must be 1.0
	// regardless of the single opponent's random hand.
	hole := cs("As", "Ks")
	board := cs("Qs", "Js", "Ts", "2c", "3d")
	got := Equity(hole, board, 1, 500, testRNG(6))
	if got != 1.0 {
		t.Fatalf("royal flush equity = %.4f, want 1.0", got)
	}
}

func TestEquityPerformance(t *testing.T) {
	hole := cs("Ah", "Kd")
	board := cs("Qs", "7h", "2c")
	rng := testRNG(7)
	start := time.Now()
	Equity(hole, board, 4, 2000, rng)
	elapsed := time.Since(start)
	// The spec's budget (well under 50ms) is for a normal build; the
	// race detector instruments every memory access and can slow a hot
	// loop like this by an order of magnitude, so it gets a looser
	// budget purely to avoid flaking under -race.
	budget := 50 * time.Millisecond
	if raceEnabled {
		budget = 500 * time.Millisecond
	}
	if elapsed > budget {
		t.Fatalf("Equity(4 opponents, 2000 iters) took %s, want under %s", elapsed, budget)
	}
}
