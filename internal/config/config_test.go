package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/whoAngeel/pokerhub/internal/player"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const personaYAML = `
id: toro
name: El Toro
temperature: 0.9
reasoning: rich
chattiness: 0.7
voice: Fanfarron.
strategy: Sube siempre.
`

func validConfigYAML() string {
	return `
server:
  addr: "127.0.0.1:8080"
  debug: false
providers:
  mock:
    type: mock
table:
  small_blind: 5
  big_blind: 10
  buy_in: 1000
  rebuy: auto
  seed: 42
  action_delay_ms: 0
  decision_timeout_ms: 1000
  equity_iters: 100
seats:
  - { type: rulebot, name: "Bot1" }
  - { type: llm, persona: personas/toro.yaml, provider: mock, model: "x" }
`
}

func TestLoadValid(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "personas/toro.yaml", personaYAML)
	path := writeFile(t, dir, "config.yaml", validConfigYAML())

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Table.SmallBlind != 5 || cfg.Table.BigBlind != 10 {
		t.Fatalf("unexpected table: %+v", cfg.Table)
	}
	if len(cfg.Seats) != 2 {
		t.Fatalf("expected 2 seats, got %d", len(cfg.Seats))
	}
	if cfg.Providers["mock"].Name != "mock" {
		t.Fatalf("provider Name not set: %+v", cfg.Providers["mock"])
	}
}

func TestLoadInvalid(t *testing.T) {
	tests := []struct {
		name  string
		yaml  string
		errIs string
	}{
		{"zero small blind", `
table: { small_blind: 0, big_blind: 10, buy_in: 1000 }
seats: [{type: rulebot}, {type: rulebot}]
`, "small_blind"},
		{"buy_in too small", `
table: { small_blind: 5, big_blind: 10, buy_in: 10 }
seats: [{type: rulebot}, {type: rulebot}]
`, "buy_in"},
		{"too few seats", `
table: { small_blind: 5, big_blind: 10, buy_in: 1000 }
seats: [{type: rulebot}]
`, "seats"},
		{"unknown provider", `
table: { small_blind: 5, big_blind: 10, buy_in: 1000 }
seats: [{type: llm, persona: personas/toro.yaml, provider: nope, model: x}, {type: rulebot}]
`, "provider"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFile(t, dir, "personas/toro.yaml", personaYAML)
			path := writeFile(t, dir, "config.yaml", tc.yaml)
			if _, err := Load(path); err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.errIs)
			}
		})
	}
}

func TestBuildMockOverride(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "personas/toro.yaml", personaYAML)
	path := writeFile(t, dir, "config.yaml", `
server: { addr: "127.0.0.1:0", debug: false }
providers:
  local:
    type: openai
    base_url: "http://example.invalid:1/v1"
table: { small_blind: 5, big_blind: 10, buy_in: 1000, seed: 1, decision_timeout_ms: 1000, equity_iters: 50 }
seats:
  - { type: rulebot, name: "Bot1" }
  - { type: llm, persona: personas/toro.yaml, provider: local, model: "x" }
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	players, err := Build(cfg, WithMockProviders())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if len(players) != 2 {
		t.Fatalf("expected 2 players, got %d", len(players))
	}
	if players[0].Kind() != "rulebot" {
		t.Fatalf("seat 0: expected rulebot, got %s", players[0].Kind())
	}
	if players[1].Kind() != "llm" {
		t.Fatalf("seat 1: expected llm, got %s", players[1].Kind())
	}
	if players[1].Name() != "El Toro" {
		t.Fatalf("seat 1: expected persona name fallback, got %q", players[1].Name())
	}

	// With WithMockProviders, the llm seat must never touch the network: a
	// Decide call against the (bogus) "local" base_url must still return a
	// Decision, proving it actually went through the mock provider.
	v := player.DecisionView{
		LegalActions: []player.LegalAction{{Type: "check"}, {Type: "fold"}},
	}
	d := players[1].Decide(t.Context(), v)
	if d.Meta.Fallback {
		t.Fatalf("unexpected fallback: %+v", d.Meta)
	}
}
