package llm_test

import (
	"testing"

	"github.com/atzomx/pokerllm/internal/llm"
	"github.com/atzomx/pokerllm/internal/llm/llmtest"
)

// TestOpenAIConformance runs the shared llmtest conformance suite against
// the "openai" provider, which is the one Provider in this package whose
// wire protocol is actually OpenAI Chat Completions.
func TestOpenAIConformance(t *testing.T) {
	llmtest.RunConformance(t, func(baseURL string) llm.Provider {
		p, err := llm.NewOpenAIProvider(llm.ProviderConfig{Type: "openai", BaseURL: baseURL})
		if err != nil {
			t.Fatal(err)
		}
		return p
	})
}
