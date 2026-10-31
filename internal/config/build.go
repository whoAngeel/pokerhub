package config

import (
	"fmt"

	"github.com/whoAngeel/pokerhub/internal/agent"
	"github.com/whoAngeel/pokerhub/internal/llm"
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
// returned slice is seat i). Every llm seat's provider is wrapped with a
// single shared llm.Queue (concurrency 1, PriorityHigh: decisions always run
// as soon as the GPU/model is free), per PLAN §8 ("un solo modelo cargado y
// compartido").
func Build(cfg *Config, opts ...BuildOption) ([]player.Player, error) {
	var o BuildOptions
	for _, opt := range opts {
		opt(&o)
	}

	queue := llm.NewQueue(1)
	providers := map[string]llm.Provider{} // provider name -> queue-wrapped Provider, built lazily and shared across seats

	players := make([]player.Player, len(cfg.Seats))
	for i, s := range cfg.Seats {
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
				return nil, fmt.Errorf("config: build seats[%d]: %w", i, err)
			}

			prov, err := providerFor(providers, cfg, s.Provider, queue, o.MockProviders)
			if err != nil {
				return nil, fmt.Errorf("config: build seats[%d]: %w", i, err)
			}

			name := s.Name
			if name == "" {
				name = persona.Name
			}
			players[i] = agent.New(name, persona, prov, s.Provider, s.Model)

		default:
			// Validate already rejects this; kept defensive in case Build
			// is ever called on a Config that bypassed Validate.
			return nil, fmt.Errorf("config: build seats[%d]: unknown seat type %q", i, s.Type)
		}
	}
	return players, nil
}

// providerFor returns the shared, queue-wrapped Provider for providerName,
// building and caching it on first use. When mock is true every provider is
// built as "mock" instead (but still cached per original name, so distinct
// seats sharing a provider name still share one instance).
func providerFor(cache map[string]llm.Provider, cfg *Config, providerName string, queue *llm.Queue, mock bool) (llm.Provider, error) {
	if p, ok := cache[providerName]; ok {
		return p, nil
	}

	pc := cfg.Providers[providerName]
	if mock {
		pc = llm.ProviderConfig{Type: "mock", Name: providerName}
	}

	base, err := llm.New(pc)
	if err != nil {
		return nil, fmt.Errorf("provider %q: %w", providerName, err)
	}
	wrapped := llm.Wrap(base, queue, llm.PriorityHigh)
	cache[providerName] = wrapped
	return wrapped, nil
}
