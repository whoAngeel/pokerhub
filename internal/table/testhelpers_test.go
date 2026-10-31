package table

import (
	"context"
	"sync"

	"github.com/whoAngeel/pokerhub/internal/agent"
	"github.com/whoAngeel/pokerhub/internal/llm"
	"github.com/whoAngeel/pokerhub/internal/player"
)

// captureSink records every Event emitted, in order, safe for concurrent use
// (Session.Run only ever emits from one goroutine in practice, but tests
// sometimes poke Pause/Resume/Step from another).
type captureSink struct {
	mu     sync.Mutex
	events []Event
}

func (c *captureSink) Emit(e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *captureSink) all() []Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Event, len(c.events))
	copy(out, c.events)
	return out
}

// recordingPlayer wraps a player.Player and records every DecisionView it's
// asked to decide on, so tests can inspect exactly what a seat was shown.
type recordingPlayer struct {
	player.Player
	mu    sync.Mutex
	views []player.DecisionView
}

func newRecordingPlayer(p player.Player) *recordingPlayer {
	return &recordingPlayer{Player: p}
}

func (r *recordingPlayer) Decide(ctx context.Context, v player.DecisionView) player.Decision {
	r.mu.Lock()
	r.views = append(r.views, v)
	r.mu.Unlock()
	return r.Player.Decide(ctx, v)
}

func (r *recordingPlayer) Views() []player.DecisionView {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]player.DecisionView, len(r.views))
	copy(out, r.views)
	return out
}

var testPersona = agent.Persona{
	ID:          "test",
	Name:        "Test",
	Temperature: 0.5,
	Reasoning:   agent.ReasoningShort,
	Chattiness:  0,
	Voice:       "voz de prueba",
	Strategy:    "estrategia de prueba",
}

// newMockAgent builds an LLMAgent over a fresh, default-scripted MockProvider
// (every Complete call returns a valid "check/0" decision, which
// agent.ParseAndNormalize swaps to whatever's actually legal).
func newMockAgent(name string) player.Player {
	p := testPersona
	p.Name = name
	return agent.New(name, p, llm.NewMock(), "mock", "mock-model")
}

func newMockAgentWithProvider(name string, prov llm.Provider) player.Player {
	p := testPersona
	p.Name = name
	return agent.New(name, p, prov, "mock", "mock-model")
}

// illegalPlayer always proposes "bet 1", which is never legal preflop once
// blinds are posted (the engine only offers "bet" when nobody has put
// anything in yet this street; preflop the blind already has) -- used to
// force table's table-level illegal-action fallback path.
type illegalPlayer struct{ name string }

func (p *illegalPlayer) Name() string { return p.name }
func (p *illegalPlayer) Kind() string { return "illegal-test" }
func (p *illegalPlayer) Decide(_ context.Context, v player.DecisionView) player.Decision {
	return player.Decision{Action: player.Action{Type: "bet", Amount: 1}}
}

// warningsOnlyPlayer always takes a safe, legal action (check, else call,
// else fold) but reports Decision.Meta.Warnings as if agent.ParseAndNormalize
// had to correct something, WITHOUT setting Meta.Fallback. Used to assert
// the table does NOT raise agent_error for mere normalization warnings
// (docs/PROTOCOL.md reserves agent_error for actual fallbacks/illegal
// actions; warnings alone stay inside the reasoning event's Meta).
type warningsOnlyPlayer struct{ name string }

func (p *warningsOnlyPlayer) Name() string { return p.name }
func (p *warningsOnlyPlayer) Kind() string { return "warnings-only-test" }
func (p *warningsOnlyPlayer) Decide(_ context.Context, v player.DecisionView) player.Decision {
	action := player.Action{Type: "fold"}
	for _, la := range v.LegalActions {
		if la.Type == "check" {
			action = player.Action{Type: "check"}
			break
		}
		if la.Type == "call" {
			action = player.Action{Type: "call", Amount: la.Amount}
		}
	}
	return player.Decision{
		Action:    action,
		Reasoning: &player.Reasoning{Hand: "x", Odds: "x", Reads: "x", Plan: "x"},
		Meta:      player.DecisionMeta{Warnings: []string{"clamped_amount"}, Fallback: false},
	}
}

func defaultConfig() Config {
	return Config{
		SmallBlind:        5,
		BigBlind:          10,
		BuyIn:             500,
		Seed:              1,
		ActionDelayMS:     0,
		DecisionTimeoutMS: 2000,
		EquityIters:       100,
	}
}

func sumStacks(m map[int]int) int {
	total := 0
	for _, v := range m {
		total += v
	}
	return total
}
