// Package llm holds model providers. Providers know nothing about poker:
// they take System/User/Schema and return text.
package llm

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

var (
	ErrRateLimit   = errors.New("llm: rate limited")
	ErrBadOutput   = errors.New("llm: bad output")
	ErrUnavailable = errors.New("llm: provider unavailable")
)

type Request struct {
	Model       string
	System      string
	User        string
	Schema      json.RawMessage // JSON Schema the output must follow; nil = free text
	SchemaName  string
	MaxTokens   int
	Temperature float64
}

type Usage struct {
	PromptTokens int
	OutputTokens int
}

type Response struct {
	Content string
	Usage   Usage
	Latency time.Duration
}

type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request) (Response, error)
}

// StructuredMode is how a provider enforces JSON output.
type StructuredMode string

const (
	StructuredJSONSchema StructuredMode = "json_schema"
	StructuredJSONObject StructuredMode = "json_object"
	StructuredPrompt     StructuredMode = "prompt"
)

// ProviderConfig mirrors one entry of `providers:` in configs/config.yaml.
type ProviderConfig struct {
	Name       string            `yaml:"-"`
	Type       string            `yaml:"type"`
	BaseURL    string            `yaml:"base_url"`
	APIKeyEnv  string            `yaml:"api_key_env"`
	Structured StructuredMode    `yaml:"structured"`
	Extra      map[string]any    `yaml:"extra"`
	Headers    map[string]string `yaml:"headers"`
}

type Factory func(cfg ProviderConfig) (Provider, error)
