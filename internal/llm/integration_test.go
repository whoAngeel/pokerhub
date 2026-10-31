//go:build integration

package llm

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestIntegration_OllamaReal hits a real local Ollama server. It is gated
// behind the "integration" build tag so `go test ./...` never touches the
// network: run explicitly with
//
//	go test -tags integration ./internal/llm/ -run Integration -v
func TestIntegration_OllamaReal(t *testing.T) {
	p, err := NewOpenAIProvider(ProviderConfig{
		Type:    "openai",
		BaseURL: "http://localhost:11434/v1",
		Extra:   map[string]any{"reasoning_effort": "none"},
	})
	if err != nil {
		t.Fatal(err)
	}

	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"action": {"type": "string", "enum": ["check", "call", "raise", "fold"]},
			"say": {"type": "string"}
		},
		"required": ["action", "say"]
	}`)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	start := time.Now()
	resp, err := p.Complete(ctx, Request{
		Model:      "gemma4:e4b",
		System:     "Eres un jugador de poker. Responde solo con JSON valido segun el schema.",
		User:       "Tienes As de picas y Rey de corazones. El bote es de 100 fichas. Decide tu accion.",
		Schema:     schema,
		SchemaName: "decision",
		MaxTokens:  200,
	})
	elapsed := time.Since(start)
	t.Logf("latency: %v", elapsed)

	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	t.Logf("content: %s", resp.Content)
	t.Logf("usage: prompt=%d output=%d", resp.Usage.PromptTokens, resp.Usage.OutputTokens)

	var decoded map[string]any
	if err := json.Unmarshal([]byte(resp.Content), &decoded); err != nil {
		t.Fatalf("response content is not valid JSON: %v\ncontent: %s", err, resp.Content)
	}
	if _, ok := decoded["action"]; !ok {
		t.Errorf("response JSON missing required \"action\" key: %s", resp.Content)
	}
}
