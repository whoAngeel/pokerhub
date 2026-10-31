// Package agent implements LLMAgent: prompt construction, the decision JSON
// schema, parsing/validation/normalization, retry and fallback (PLAN §4). It
// never imports internal/engine: it only talks to player.DecisionView /
// player.Decision (DTOs) and llm.Provider (text in, text out).
package agent

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ReasoningStyle selects how much the model is allowed to write in
// "reasoning" (PLAN §4.2/§4.3): "rich" gets a 450 token budget, "short" 180.
const (
	ReasoningRich  = "rich"
	ReasoningShort = "short"
)

// Persona mirrors one configs/personas/*.yaml file (PLAN §6).
type Persona struct {
	ID          string  `yaml:"id"`
	Name        string  `yaml:"name"`
	Temperature float64 `yaml:"temperature"`
	Reasoning   string  `yaml:"reasoning"` // "rich" | "short"
	Chattiness  float64 `yaml:"chattiness"`
	Voice       string  `yaml:"voice"`
	Strategy    string  `yaml:"strategy"`
}

// LoadPersona reads and validates a persona YAML file.
func LoadPersona(path string) (Persona, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Persona{}, fmt.Errorf("agent: read persona %s: %w", path, err)
	}
	var p Persona
	if err := yaml.Unmarshal(data, &p); err != nil {
		return Persona{}, fmt.Errorf("agent: parse persona %s: %w", path, err)
	}
	if err := p.Validate(); err != nil {
		return Persona{}, fmt.Errorf("agent: invalid persona %s: %w", path, err)
	}
	return p, nil
}

// Validate checks that a Persona is complete and within sane ranges.
func (p Persona) Validate() error {
	if p.ID == "" {
		return fmt.Errorf("missing id")
	}
	if p.Name == "" {
		return fmt.Errorf("missing name")
	}
	if p.Reasoning != ReasoningRich && p.Reasoning != ReasoningShort {
		return fmt.Errorf("reasoning must be %q or %q, got %q", ReasoningRich, ReasoningShort, p.Reasoning)
	}
	if p.Temperature < 0 || p.Temperature > 2 {
		return fmt.Errorf("temperature out of range [0,2]: %v", p.Temperature)
	}
	if p.Chattiness < 0 || p.Chattiness > 1 {
		return fmt.Errorf("chattiness out of range [0,1]: %v", p.Chattiness)
	}
	if p.Voice == "" {
		return fmt.Errorf("missing voice")
	}
	if p.Strategy == "" {
		return fmt.Errorf("missing strategy")
	}
	return nil
}

// MaxTokens returns the output token budget for this persona's reasoning
// style (PLAN §4.2: rich=450, short=180).
func (p Persona) MaxTokens() int {
	if p.Reasoning == ReasoningShort {
		return 180
	}
	return 450
}
