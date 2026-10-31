package config

import (
	"fmt"

	"github.com/whoAngeel/pokerhub/internal/agent"
	"github.com/whoAngeel/pokerhub/internal/llm"
	"github.com/whoAngeel/pokerhub/internal/memory"
	"github.com/whoAngeel/pokerhub/internal/player"
)

// BuildOptions configures Build. The zero value is the normal behavior
// (build providers exactly as configured).
type BuildOptions struct {
	// MockProviders overrides every seat's provider with "mock", regardless
	// of what configs/config.yaml says. Used for tests and --mock sim runs
	// that must not touch the network or a GPU.
	MockProviders bool
}

// BuildOption mutates a BuildOptions.
type BuildOption func(*BuildOptions)

// WithMockProviders forces every llm seat to use the mock provider.
func WithMockProviders() BuildOption {
	return func(o *BuildOptions) { o.MockProviders = true }
}

// Build constructs one player.Player per seat, in seat order (index i of the
// returned slice is seat i), plus one memory.AgentInfo per seat (same index)
// for table.Session.SetMemoryAgents (PLAN §7). Every llm seat's provider is
// wrapped with a single shared llm.Queue: decisions always run at
// llm.PriorityHigh (PLAN §8, "un solo modelo cargado y compartido"), and,
// when cfg.Table.MemoryNotes is true, the SAME underlying provider is also
// wrapped at llm.PriorityLow for that seat's memory.AgentInfo, so a pending
// notes update never delays a decision.
//
// memAgents[i].Provider is nil for every non-llm seat and for every llm seat
// when cfg.Table.MemoryNotes is false: table.Session treats a nil Provider
// as "memory notes disabled for this seat" and skips it entirely.
func Build(cfg *Config, opts ...BuildOption) ([]player.Player, []memory.AgentInfo, error) {
	var o BuildOptions
	for _, opt := range opts {
		opt(&o)
	}

	queue := llm.NewQueue(1)
	bases := map[string]llm.Provider{}     // provider name -> raw (unwrapped) Provider, shared across priorities
	highCache := map[string]llm.Provider{} // provider name -> queue-wrapped at PriorityHigh
	lowCache := map[string]llm.Provider{}  // provider name -> queue-wrapped at PriorityLow

	players := make([]player.Player, len(cfg.Seats))
	memAgents := make([]memory.AgentInfo, len(cfg.Seats))

	for i, s := range cfg.Seats {
		memAgents[i].Seat = i

		switch s.Type {
		case SeatRuleBot:
			name := s.Name
			if name == "" {
				name = fmt.Sprintf("Seat %d", i)
			}
			players[i] = player.NewRuleBot(name)

		case SeatLLM:
			persona, err := agent.LoadPersona(cfg.PersonaPath(s.Persona))
			if err != nil {
				return nil, nil, fmt.Errorf("config: build seats[%d]: %w", i, err)
			}

			provHigh, err := providerFor(bases, highCache, cfg, s.Provider, queue, llm.PriorityHigh, o.MockProviders)
			if err != nil {
				return nil, nil, fmt.Errorf("config: build seats[%d]: %w", i, err)
			}

			name := s.Name
			if name == "" {
				name = persona.Name
			}
			players[i] = agent.New(name, persona, provHigh, s.Provider, s.Model)

			if cfg.Table.MemoryNotes {
				provLow, err := providerFor(bases, lowCache, cfg, s.Provider, queue, llm.PriorityLow, o.MockProviders)
				if err != nil {
					return nil, nil, fmt.Errorf("config: build seats[%d]: %w", i, err)
				}
				memAgents[i] = memory.AgentInfo{
					Seat:        i,
					PersonaName: persona.Name,
					Provider:    provLow,
					Model:       s.Model,
				}
			}

		default:
			// Validate already rejects this; kept defensive in case Build
			// is ever called on a Config that bypassed Validate.
			return nil, nil, fmt.Errorf("config: build seats[%d]: unknown seat type %q", i, s.Type)
		}
	}
	return players, memAgents, nil
}

// providerFor returns the queue-wrapped Provider for providerName at prio,
// building (and caching, by priority) on first use. The underlying base
// Provider is itself cached and shared across both priorities, so a
// seat's decision calls (PriorityHigh) and its memory-notes calls
// (PriorityLow) go through the exact same llm.Provider instance, just
// wrapped around the same Queue at a different priority. When mock is
// true every provider is built as "mock" instead (still cached per
// original name, so distinct seats sharing a provider name still share one
// instance).
func providerFor(bases, cache map[string]llm.Provider, cfg *Config, providerName string, queue *llm.Queue, prio llm.Priority, mock bool) (llm.Provider, error) {
	if p, ok := cache[providerName]; ok {
		return p, nil
	}

	base, ok := bases[providerName]
	if !ok {
		pc := cfg.Providers[providerName]
		if mock {
			pc = llm.ProviderConfig{Type: "mock", Name: providerName}
		}
		b, err := llm.New(pc)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", providerName, err)
		}
		base = b
		bases[providerName] = base
	}

	wrapped := llm.Wrap(base, queue, prio)
	cache[providerName] = wrapped
	return wrapped, nil
}
