// Package config loads configs/config.yaml into typed structs and builds the
// table's players from it (PLAN §6). It is the only package allowed to wire
// internal/llm, internal/agent and internal/player together from YAML.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/whoAngeel/pokerhub/internal/agent"
	"github.com/whoAngeel/pokerhub/internal/llm"
)

// Server mirrors configs/config.yaml's `server:` block.
type Server struct {
	Addr  string `yaml:"addr"`
	Debug bool   `yaml:"debug"`
}

// Table mirrors configs/config.yaml's `table:` block.
type Table struct {
	SmallBlind        int    `yaml:"small_blind"`
	BigBlind          int    `yaml:"big_blind"`
	BuyIn             int    `yaml:"buy_in"`
	Rebuy             string `yaml:"rebuy"` // "auto" is the only mode F2 supports
	Seed              int64  `yaml:"seed"`  // 0 = time-based seed (chosen at Build/Session time, never inside internal/engine)
	ActionDelayMS     int    `yaml:"action_delay_ms"`
	DecisionTimeoutMS int    `yaml:"decision_timeout_ms"`
	EquityIters       int    `yaml:"equity_iters"`
	// MemoryNotes toggles PLAN §7's per-agent private notes about rivals
	// (table.memory_notes in configs/config.yaml, default true there).
	// false disables it entirely: Build then hands table.Session no
	// memory.AgentInfo at all.
	MemoryNotes bool `yaml:"memory_notes"`
}

// SeatConfig mirrors one entry of configs/config.yaml's `seats:` list.
type SeatConfig struct {
	Type     string `yaml:"type"` // "rulebot" | "llm"
	Name     string `yaml:"name"` // display name; for llm seats, defaults to the persona's name when empty
	Persona  string `yaml:"persona"`
	Provider string `yaml:"provider"`
	Model    string `yaml:"model"`
}

const (
	SeatRuleBot = "rulebot"
	SeatLLM     = "llm"
)

// Config is the parsed, validated contents of configs/config.yaml.
type Config struct {
	Server    Server                        `yaml:"server"`
	Providers map[string]llm.ProviderConfig `yaml:"providers"`
	Table     Table                         `yaml:"table"`
	Seats     []SeatConfig                  `yaml:"seats"`

	dir string // directory containing the loaded file; persona paths resolve relative to it
}

// Load reads and validates path. Persona paths in seats are resolved
// relative to path's directory, not the process's working directory, so the
// config can be run from anywhere.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: read %s: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %s: %w", path, err)
	}
	cfg.dir = filepath.Dir(path)

	// ProviderConfig.Name is tagged yaml:"-" (set by us, never by the file)
	// so llm providers can report a stable display name in errors/Meta.
	for name, pc := range cfg.Providers {
		pc.Name = name
		cfg.Providers[name] = pc
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config: invalid %s: %w", path, err)
	}
	return &cfg, nil
}

// PersonaPath resolves a seat's persona path relative to the directory the
// config file was loaded from.
func (c *Config) PersonaPath(persona string) string {
	if filepath.IsAbs(persona) {
		return persona
	}
	return filepath.Join(c.dir, persona)
}

// Validate checks structural and cross-referential sanity: blinds positive,
// buy-in above the big blind, 2-9 seats, every llm seat's provider exists
// and persona loads.
func (c *Config) Validate() error {
	if c.Table.SmallBlind <= 0 {
		return fmt.Errorf("table.small_blind must be > 0, got %d", c.Table.SmallBlind)
	}
	if c.Table.BigBlind <= 0 {
		return fmt.Errorf("table.big_blind must be > 0, got %d", c.Table.BigBlind)
	}
	if c.Table.BigBlind < c.Table.SmallBlind {
		return fmt.Errorf("table.big_blind (%d) must be >= table.small_blind (%d)", c.Table.BigBlind, c.Table.SmallBlind)
	}
	if c.Table.BuyIn <= c.Table.BigBlind {
		return fmt.Errorf("table.buy_in (%d) must be > table.big_blind (%d)", c.Table.BuyIn, c.Table.BigBlind)
	}
	if n := len(c.Seats); n < 2 || n > 9 {
		return fmt.Errorf("seats: need 2-9 seats, got %d", n)
	}

	for i, s := range c.Seats {
		switch s.Type {
		case SeatRuleBot:
			// no further requirements
		case SeatLLM:
			if s.Provider == "" {
				return fmt.Errorf("seats[%d]: llm seat missing provider", i)
			}
			if _, ok := c.Providers[s.Provider]; !ok {
				return fmt.Errorf("seats[%d]: unknown provider %q", i, s.Provider)
			}
			if s.Persona == "" {
				return fmt.Errorf("seats[%d]: llm seat missing persona", i)
			}
			if _, err := agent.LoadPersona(c.PersonaPath(s.Persona)); err != nil {
				return fmt.Errorf("seats[%d]: %w", i, err)
			}
		default:
			return fmt.Errorf("seats[%d]: unknown seat type %q", i, s.Type)
		}
	}
	return nil
}
