//go:build integration

package agent

import (
	"context"
	"testing"
	"time"

	"github.com/atzomx/pokerllm/internal/llm"
	"github.com/atzomx/pokerllm/internal/player"
)

// TestIntegration_OllamaRealDecision hits a real local Ollama server with a
// full LLMAgent (persona configs/personas/profesor.yaml, a hand-written flop
// DecisionView facing a bet). Gated behind the "integration" build tag so
// `go test ./...` never touches the network:
//
//	go test -tags integration ./internal/agent/ -run Integration -v
func TestIntegration_OllamaRealDecision(t *testing.T) {
	prov, err := llm.NewOpenAIProvider(llm.ProviderConfig{
		Type:    "openai",
		BaseURL: "http://localhost:11434/v1",
		Extra:   map[string]any{"reasoning_effort": "none"},
	})
	if err != nil {
		t.Fatal(err)
	}

	persona, err := LoadPersona("../../configs/personas/profesor.yaml")
	if err != nil {
		t.Fatal(err)
	}

	a := New("El Profesor", persona, prov, "local", "gemma4:e4b")

	// Flop: AhKd on Ks7h2c, facing a bet of 40 into a pot of 90.
	v := player.DecisionView{
		HandID: 1,
		Street: "flop",
		Button: 0,
		You:    player.Me{Seat: 2, Name: persona.Name, Hole: []string{"Ah", "Kd"}, Stack: 860, InPot: 50},
		Board:  []string{"Ks", "7h", "2c"},
		Pot:    90,
		ToCall: 40,
		Helpers: player.Helpers{
			MadeHand: "pareja de reyes, kicker as",
			Equity:   0.68,
			PotOdds:  0.31,
		},
		ActionLog: "preflop: P0 raise 30, P2 call | flop: P0 bet 40",
		Opponents: []player.Opponent{
			{Seat: 0, Name: "La Abuela", Stack: 900, Status: "active"},
		},
		LegalActions: []player.LegalAction{
			{Type: "fold"},
			{Type: "call", Amount: 40},
			{Type: "raise", Min: 120, Max: 860},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	d := a.Decide(ctx, v)
	elapsed := time.Since(start)

	t.Logf("latency: %v (Meta.LatencyMS=%d)", elapsed, d.Meta.LatencyMS)
	t.Logf("action: %s amount=%d", d.Action.Type, d.Action.Amount)
	t.Logf("say: %q", d.Say)
	t.Logf("warnings: %v", d.Meta.Warnings)
	t.Logf("retries: %d, fallback: %v, tokens_out: %d", d.Meta.Retries, d.Meta.Fallback, d.Meta.TokensOut)
	if d.Reasoning != nil {
		t.Logf("reasoning.hand: %s", d.Reasoning.Hand)
		t.Logf("reasoning.odds: %s", d.Reasoning.Odds)
		t.Logf("reasoning.reads: %s", d.Reasoning.Reads)
		t.Logf("reasoning.plan: %s", d.Reasoning.Plan)
	}

	if d.Meta.Fallback {
		t.Errorf("Decide fell back to %+v (warnings=%v); expected a real model decision", d.Action, d.Meta.Warnings)
	}
	if d.Reasoning == nil {
		t.Fatal("Reasoning is nil; expected the model's structured reasoning")
	}
	validActionSet := map[string]bool{"fold": true, "call": true, "raise": true, "all_in": true}
	if !validActionSet[d.Action.Type] {
		t.Errorf("Action.Type = %q, want one of fold/call/raise/all_in", d.Action.Type)
	}
}
