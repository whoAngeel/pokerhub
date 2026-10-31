package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
)

// defaultSchemaName is used for the json_schema response_format's name when
// Request.SchemaName is empty.
const defaultSchemaName = "output"

// schemaInstructionTemplate is appended to the system prompt for the
// json_object and prompt structured modes, which don't get strict schema
// enforcement from the server and so need the schema spelled out in words.
const schemaInstructionTemplate = "Responde SOLO con un objeto JSON que cumpla este JSON Schema:\n%s"

func init() {
	Register("openai", NewOpenAIProvider)
}

// openaiProvider talks to any OpenAI-compatible Chat Completions endpoint:
// OpenAI itself, Ollama, vLLM, llama.cpp server, LM Studio, OpenRouter, Groq,
// Together, etc. It knows nothing about poker; see internal/llm doc comment.
type openaiProvider struct {
	name       string
	client     openai.Client
	structured StructuredMode
}

// NewOpenAIProvider builds a Provider for cfg.Type == "openai". The API key
// comes from os.Getenv(cfg.APIKeyEnv); if APIKeyEnv is empty, a dummy key
// ("ollama") is used because local servers ignore it; if APIKeyEnv is set but
// the named env var is empty, construction fails loudly rather than silently
// sending an empty key.
func NewOpenAIProvider(cfg ProviderConfig) (Provider, error) {
	apiKey := "ollama"
	if cfg.APIKeyEnv != "" {
		v := os.Getenv(cfg.APIKeyEnv)
		if v == "" {
			return nil, fmt.Errorf("llm: provider %q: env var %s (api_key_env) is not set", displayName(cfg), cfg.APIKeyEnv)
		}
		apiKey = v
	}

	opts := []option.RequestOption{
		option.WithAPIKey(apiKey),
		// The agent layer owns retries (backoff on ErrRateLimit, fallback
		// otherwise); the SDK must not retry behind our backs.
		option.WithMaxRetries(0),
	}
	if cfg.BaseURL != "" {
		opts = append(opts, option.WithBaseURL(cfg.BaseURL))
	}
	for k, v := range cfg.Headers {
		opts = append(opts, option.WithHeader(k, v))
	}
	for k, v := range cfg.Extra {
		// This is how `reasoning_effort: none` (and any other server-specific
		// body field) reaches the request. Critical: without this, a local
		// model's native thinking stays on and burns the whole token budget.
		opts = append(opts, option.WithJSONSet(k, v))
	}

	structured := cfg.Structured
	if structured == "" {
		structured = StructuredJSONSchema
	}

	return &openaiProvider{
		name:       displayName(cfg),
		client:     openai.NewClient(opts...),
		structured: structured,
	}, nil
}

func displayName(cfg ProviderConfig) string {
	if cfg.Name != "" {
		return cfg.Name
	}
	if cfg.Type != "" {
		return cfg.Type
	}
	return "openai"
}

func (p *openaiProvider) Name() string { return p.name }

func (p *openaiProvider) Complete(ctx context.Context, req Request) (Response, error) {
	system := req.System

	params := openai.ChatCompletionNewParams{
		Model: req.Model,
	}

	if req.Schema != nil {
		schemaMap, err := schemaToMap(req.Schema)
		if err != nil {
			return Response{}, fmt.Errorf("llm: invalid Request.Schema: %w", err)
		}

		switch p.structured {
		case StructuredJSONObject:
			params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
				OfJSONObject: &shared.ResponseFormatJSONObjectParam{},
			}
			system = appendSchemaInstruction(system, schemaMap)
		case StructuredPrompt:
			system = appendSchemaInstruction(system, schemaMap)
		default: // StructuredJSONSchema
			name := req.SchemaName
			if name == "" {
				name = defaultSchemaName
			}
			params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{
				OfJSONSchema: &shared.ResponseFormatJSONSchemaParam{
					JSONSchema: shared.ResponseFormatJSONSchemaJSONSchemaParam{
						Name:   name,
						Strict: param.NewOpt(true),
						Schema: schemaMap,
					},
				},
			}
		}
	}

	params.Messages = []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage(system),
		openai.UserMessage(req.User),
	}
	if req.MaxTokens > 0 {
		params.MaxTokens = param.NewOpt(int64(req.MaxTokens))
	}
	params.Temperature = param.NewOpt(req.Temperature)

	start := time.Now()
	completion, err := p.client.Chat.Completions.New(ctx, params)
	latency := time.Since(start)
	if err != nil {
		return Response{}, mapOpenAIError(ctx, err)
	}
	if len(completion.Choices) == 0 {
		return Response{}, fmt.Errorf("%w: provider returned no choices", ErrBadOutput)
	}

	message := completion.Choices[0].Message
	content := message.Content

	if strings.TrimSpace(content) == "" {
		if reasoning := extractReasoningField(message); reasoning != "" {
			return Response{}, fmt.Errorf("%w: empty content but model emitted a reasoning field (%q); thinking enabled? set extra.reasoning_effort: none",
				ErrBadOutput, truncate(reasoning, 200))
		}
	}

	if req.Schema != nil && p.structured == StructuredPrompt {
		extracted, err := extractBalancedJSON(content)
		if err != nil {
			return Response{}, fmt.Errorf("%w: %v", ErrBadOutput, err)
		}
		content = extracted
	}

	return Response{
		Content: content,
		Usage: Usage{
			PromptTokens: int(completion.Usage.PromptTokens),
			OutputTokens: int(completion.Usage.CompletionTokens),
		},
		Latency: latency,
	}, nil
}

func schemaToMap(raw json.RawMessage) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
}

func appendSchemaInstruction(system string, schema map[string]any) string {
	b, err := json.Marshal(schema)
	if err != nil {
		// schema was already successfully unmarshaled from JSON, so a
		// marshal error here would mean a non-JSON-safe value (e.g. a NaN
		// snuck in some other way); fall back to a best-effort message.
		b = []byte(fmt.Sprintf("%v", schema))
	}
	instruction := fmt.Sprintf(schemaInstructionTemplate, string(b))
	if system == "" {
		return instruction
	}
	return system + "\n\n" + instruction
}

// extractReasoningField reads a non-standard "reasoning" field some
// OpenAI-compatible servers (Ollama, vLLM with reasoning models) attach to
// the message object when thinking wasn't actually turned off server-side.
func extractReasoningField(message openai.ChatCompletionMessage) string {
	field, ok := message.JSON.ExtraFields["reasoning"]
	if !ok {
		return ""
	}
	raw := field.Raw()
	if raw == "" || raw == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal([]byte(raw), &s); err == nil {
		return s
	}
	return raw
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// mapOpenAIError normalizes SDK/transport errors to the sentinels the agent
// layer switches on. ctx cancellation always wins, so errors.Is(err,
// context.Canceled) / errors.Is(err, context.DeadlineExceeded) works even
// though the SDK wraps the underlying error in its own type.
func mapOpenAIError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}

	var apiErr *openai.Error
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == http.StatusTooManyRequests:
			return fmt.Errorf("%w: %s", ErrRateLimit, apiErr.Message)
		case apiErr.StatusCode >= 500:
			return fmt.Errorf("%w: http %d: %s", ErrUnavailable, apiErr.StatusCode, apiErr.Message)
		default:
			return fmt.Errorf("llm: openai request failed: %w", err)
		}
	}

	// Connection refused, DNS failure, dial/read timeouts, etc. all surface
	// as errors satisfying net.Error (directly, or wrapped in *url.Error from
	// net/http), never as an *openai.Error, since no HTTP response came back.
	var netErr net.Error
	if errors.As(err, &netErr) {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}

	return fmt.Errorf("llm: openai request failed: %w", err)
}
