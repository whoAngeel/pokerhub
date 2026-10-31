package table

import (
	"context"
	"testing"
	"time"

	"github.com/whoAngeel/pokerhub/internal/llm"
	"github.com/whoAngeel/pokerhub/internal/player"
)

// TestSession_IllegalActionFallback uses a player that always proposes
// "bet 1" (never legal preflop once blinds are posted) and asserts the
// table catches it: falls back to check-if-legal-else-fold, keeps playing,
// and reports an agent_error with the illegal_action cause.
func TestSession_IllegalActionFallback(t *testing.T) {
	cfg := defaultConfig()
	players := []player.Player{
		player.NewRuleBot("Bot0"),
		&illegalPlayer{name: "Illegal1"},
		player.NewRuleBot("Bot2"),
	}
	sink := &captureSink{}
	sess, err := NewSession(cfg, players, sink)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := sess.Run(context.Background(), 1); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var sawIllegal, sawHandEnd bool
	for _, e := range sink.all() {
		if e.Type == EvAgentError && e.Seat == 1 {
			if len(e.Cause) >= len("illegal_action:") && e.Cause[:len("illegal_action:")] == "illegal_action:" {
				sawIllegal = true
			}
		}
		if e.Type == EvHandEnd {
			sawHandEnd = true
		}
	}
	if !sawIllegal {
		t.Fatalf("expected an illegal_action agent_error for seat 1, events: %+v", sink.all())
	}
	if !sawHandEnd {
		t.Fatalf("hand never completed after the illegal action")
	}
}

// TestSession_DecisionTimeoutFallback makes the LLM provider far slower
// than the configured decision timeout, so LLMAgent itself falls back
// (Meta.Fallback = true) and the table must surface that via agent_error
// while still completing the hand.
func TestSession_DecisionTimeoutFallback(t *testing.T) {
	cfg := defaultConfig()
	cfg.DecisionTimeoutMS = 20 // far shorter than the mock provider's delay below

	slow := llm.NewMock(llm.MockResponse{Content: "OK"}, // consumed by Session warmup
		llm.MockResponse{
			Content: `{"reasoning":{"hand":"x","odds":"x","reads":"x","plan":"x"},"action":"check","amount":0,"say":""}`,
			Delay:   300 * time.Millisecond,
		})

	players := []player.Player{
		player.NewRuleBot("Bot0"),
		newMockAgentWithProvider("Slow1", slow),
		player.NewRuleBot("Bot2"),
	}
	sink := &captureSink{}
	sess, err := NewSession(cfg, players, sink)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := sess.Run(context.Background(), 1); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var sawTimeoutFallback, sawHandEnd bool
	for _, e := range sink.all() {
		if e.Type == EvAgentError && e.Seat == 1 && e.Meta.Fallback {
			sawTimeoutFallback = true
		}
		if e.Type == EvHandEnd {
			sawHandEnd = true
		}
	}
	if !sawTimeoutFallback {
		t.Fatalf("expected a fallback agent_error for seat 1, events: %+v", sink.all())
	}
	if !sawHandEnd {
		t.Fatalf("hand never completed after the timeout fallback")
	}
}

// TestSession_WarningsOnlyDecisionIsNotAgentError asserts that a decision
// which only carries Meta.Warnings (no Meta.Fallback, no illegal action)
// never produces an agent_error event: per docs/PROTOCOL.md, agent_error is
// reserved for an actual fallback or a table-level illegal-action override.
// Plain warnings stay inside the reasoning event's Meta.
func TestSession_WarningsOnlyDecisionIsNotAgentError(t *testing.T) {
	cfg := defaultConfig()
	players := []player.Player{
		player.NewRuleBot("Bot0"),
		&warningsOnlyPlayer{name: "Warny1"},
		player.NewRuleBot("Bot2"),
	}
	sink := &captureSink{}
	sess, err := NewSession(cfg, players, sink)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := sess.Run(context.Background(), 3); err != nil {
		t.Fatalf("Run: %v", err)
	}

	var sawReasoningWithWarnings bool
	for _, e := range sink.all() {
		if e.Type == EvAgentError && e.Seat == 1 {
			t.Fatalf("warnings-only decision must not raise agent_error: %+v", e)
		}
		if e.Type == EvReasoning && e.Seat == 1 && len(e.Meta.Warnings) > 0 {
			sawReasoningWithWarnings = true
		}
	}
	if !sawReasoningWithWarnings {
		t.Fatalf("expected seat 1's warnings to still show up on its reasoning event")
	}
}
