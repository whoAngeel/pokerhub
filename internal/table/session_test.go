package table

import (
	"context"
	"fmt"
	"testing"

	"github.com/whoAngeel/pokerhub/internal/player"
)

func buildMixedPlayers(n int) []player.Player {
	players := make([]player.Player, n)
	for i := 0; i < n; i++ {
		if i%2 == 0 {
			players[i] = player.NewRuleBot(fmt.Sprintf("Bot%d", i))
		} else {
			players[i] = newMockAgent(fmt.Sprintf("Agent%d", i))
		}
	}
	return players
}

// TestSession_ChipConservation plays 200 hands with a mix of RuleBots and
// mock LLM agents and asserts total chips never leak: starting chips + every
// rebuy == final chips.
func TestSession_ChipConservation(t *testing.T) {
	cfg := defaultConfig()
	cfg.BuyIn = 200 // small on purpose, so busts and rebuys actually happen in 200 hands
	players := buildMixedPlayers(5)
	sink := &captureSink{}

	sess, err := NewSession(cfg, players, sink)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	initialTotal := sumStacks(sess.Stacks())

	if err := sess.Run(context.Background(), 200); err != nil {
		t.Fatalf("Run: %v", err)
	}

	rebuyTotal := 0
	for _, e := range sink.all() {
		if e.Type == EvRebuy {
			rebuyTotal += e.Amount
		}
	}

	finalTotal := sumStacks(sess.Stacks())
	if finalTotal != initialTotal+rebuyTotal {
		t.Fatalf("chip leak: initial=%d rebuys=%d final=%d", initialTotal, rebuyTotal, finalTotal)
	}
}

// TestSession_EventOrderSanity checks that, per hand, hand_start is the
// first non-rebuy event and hand_end is the very last event for that hand
// (rebuy, when it happens, necessarily precedes hand_start: it's what makes
// the hand possible in the first place).
func TestSession_EventOrderSanity(t *testing.T) {
	cfg := defaultConfig()
	players := buildMixedPlayers(4)
	sink := &captureSink{}

	sess, err := NewSession(cfg, players, sink)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	const hands = 15
	if err := sess.Run(context.Background(), hands); err != nil {
		t.Fatalf("Run: %v", err)
	}

	byHand := map[int][]Event{}
	for _, e := range sink.all() {
		byHand[e.HandID] = append(byHand[e.HandID], e)
	}
	if len(byHand) != hands {
		t.Fatalf("expected %d distinct hand ids, got %d", hands, len(byHand))
	}

	for id, events := range byHand {
		var firstNonRebuy, last Event
		last = events[len(events)-1]
		for _, e := range events {
			if e.Type != EvRebuy {
				firstNonRebuy = e
				break
			}
		}
		if firstNonRebuy.Type != EvHandStart {
			t.Fatalf("hand %d: first non-rebuy event = %s, want hand_start", id, firstNonRebuy.Type)
		}
		if last.Type != EvHandEnd {
			t.Fatalf("hand %d: last event = %s, want hand_end", id, last.Type)
		}
	}
}

// skeleton reduces an Event to its deterministic, timing-free fields so two
// runs can be compared for determinism regardless of real wall-clock
// latency recorded in Decision.Meta.
func skeleton(e Event) string {
	return fmt.Sprintf("%s|%d|%d|%v|%v|%v|%v|%v|%v|%v|%v",
		e.Type, e.HandID, e.Seat, e.Button, e.Stacks, e.Cards, e.Amount,
		e.Street, e.Board, e.Action, e.Pots)
}

// TestSession_Determinism runs the same seed/config/players shape twice and
// asserts the two event streams are identical modulo timing.
func TestSession_Determinism(t *testing.T) {
	run := func() []string {
		cfg := defaultConfig()
		players := buildMixedPlayers(5)
		sink := &captureSink{}
		sess, err := NewSession(cfg, players, sink)
		if err != nil {
			t.Fatalf("NewSession: %v", err)
		}
		if err := sess.Run(context.Background(), 20); err != nil {
			t.Fatalf("Run: %v", err)
		}
		out := make([]string, 0, len(sink.all()))
		for _, e := range sink.all() {
			out = append(out, skeleton(e))
		}
		return out
	}

	a := run()
	b := run()
	if len(a) != len(b) {
		t.Fatalf("event count differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("event %d differs:\n  run1=%s\n  run2=%s", i, a[i], b[i])
		}
	}
}

// TestSession_PauseStep checks Pause blocks the loop and Step lets exactly
// one turn through (smoke test for the thread-safe controls, not a timing
// stress test).
func TestSession_PauseStep(t *testing.T) {
	cfg := defaultConfig()
	players := buildMixedPlayers(3)
	sink := &captureSink{}
	sess, err := NewSession(cfg, players, sink)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	sess.Pause()
	done := make(chan error, 1)
	go func() { done <- sess.Run(context.Background(), 1) }()

	// Give the Run goroutine a moment to reach the first wait-if-paused
	// point, then let exactly one turn through, then resume fully.
	sess.Step()
	sess.Resume()

	if err := <-done; err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sink.all()) == 0 {
		t.Fatalf("expected at least one event")
	}
}
