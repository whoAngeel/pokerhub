package llm

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureServer returns an httptest.Server that decodes every request body
// into *captured (overwritten per request) and *headers, then replies with a
// minimal valid chat completion.
func captureServer(t *testing.T) (srv *httptest.Server, body *map[string]any, headers *http.Header) {
	t.Helper()
	var capturedBody map[string]any
	var capturedHeaders http.Header
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"x","object":"chat.completion","created":1,"model":"m",
			"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"{\"action\":\"check\"}"}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	t.Cleanup(srv.Close)
	return srv, &capturedBody, &capturedHeaders
}

const decisionSchema = `{"type":"object","properties":{"action":{"type":"string"}},"required":["action"]}`

func TestOpenAI_ResponseFormat_JSONSchema(t *testing.T) {
	srv, body, _ := captureServer(t)
	p, err := NewOpenAIProvider(ProviderConfig{Type: "openai", BaseURL: srv.URL, Structured: StructuredJSONSchema})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(t.Context(), Request{
		Model: "m", System: "s", User: "u",
		Schema: json.RawMessage(decisionSchema), SchemaName: "decision",
	}); err != nil {
		t.Fatal(err)
	}

	rf, ok := (*body)["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("response_format missing or wrong shape: %#v", (*body)["response_format"])
	}
	if rf["type"] != "json_schema" {
		t.Errorf("response_format.type = %v, want json_schema", rf["type"])
	}
	js, ok := rf["json_schema"].(map[string]any)
	if !ok {
		t.Fatalf("response_format.json_schema missing or wrong shape: %#v", rf["json_schema"])
	}
	if js["name"] != "decision" {
		t.Errorf("response_format.json_schema.name = %v, want decision", js["name"])
	}
	if js["strict"] != true {
		t.Errorf("response_format.json_schema.strict = %v, want true", js["strict"])
	}
	if _, ok := js["schema"].(map[string]any); !ok {
		t.Errorf("response_format.json_schema.schema missing or wrong shape: %#v", js["schema"])
	}
}

func TestOpenAI_ResponseFormat_JSONSchema_DefaultName(t *testing.T) {
	srv, body, _ := captureServer(t)
	p, err := NewOpenAIProvider(ProviderConfig{Type: "openai", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(t.Context(), Request{Model: "m", System: "s", User: "u", Schema: json.RawMessage(decisionSchema)}); err != nil {
		t.Fatal(err)
	}
	rf := (*body)["response_format"].(map[string]any)
	js := rf["json_schema"].(map[string]any)
	if js["name"] != "output" {
		t.Errorf("default SchemaName: json_schema.name = %v, want output", js["name"])
	}
}

func TestOpenAI_ResponseFormat_JSONObject(t *testing.T) {
	srv, body, _ := captureServer(t)
	p, err := NewOpenAIProvider(ProviderConfig{Type: "openai", BaseURL: srv.URL, Structured: StructuredJSONObject})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(t.Context(), Request{Model: "m", System: "sys", User: "u", Schema: json.RawMessage(decisionSchema)}); err != nil {
		t.Fatal(err)
	}

	rf, ok := (*body)["response_format"].(map[string]any)
	if !ok {
		t.Fatalf("response_format missing or wrong shape: %#v", (*body)["response_format"])
	}
	if rf["type"] != "json_object" {
		t.Errorf("response_format.type = %v, want json_object", rf["type"])
	}
	if _, hasSchema := rf["json_schema"]; hasSchema {
		t.Errorf("json_object mode should not carry a json_schema key: %#v", rf)
	}

	messages := (*body)["messages"].([]any)
	sysMsg := messages[0].(map[string]any)
	sysContent, _ := sysMsg["content"].(string)
	if !strings.Contains(sysContent, "Responde SOLO con un objeto JSON") {
		t.Errorf("system message does not carry schema instruction: %q", sysContent)
	}
	if !strings.Contains(sysContent, "sys") {
		t.Errorf("system message dropped the original system prompt: %q", sysContent)
	}
}

func TestOpenAI_ResponseFormat_Prompt(t *testing.T) {
	srv, body, _ := captureServer(t)
	p, err := NewOpenAIProvider(ProviderConfig{Type: "openai", BaseURL: srv.URL, Structured: StructuredPrompt})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(t.Context(), Request{Model: "m", System: "sys", User: "u", Schema: json.RawMessage(decisionSchema)}); err != nil {
		t.Fatal(err)
	}

	if _, has := (*body)["response_format"]; has {
		t.Errorf("prompt mode should not send response_format, got: %#v", (*body)["response_format"])
	}
	messages := (*body)["messages"].([]any)
	sysMsg := messages[0].(map[string]any)
	sysContent, _ := sysMsg["content"].(string)
	if !strings.Contains(sysContent, "Responde SOLO con un objeto JSON") {
		t.Errorf("system message does not carry schema instruction: %q", sysContent)
	}
}

func TestOpenAI_ResponseFormat_Prompt_ExtractsFencedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"x","object":"chat.completion","created":1,"model":"m",
			"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"Here you go:\n` +
			"```json\\n{\\\"action\\\": \\\"fold\\\"}\\n```" + `\nHope that helps."}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer srv.Close()

	p, err := NewOpenAIProvider(ProviderConfig{Type: "openai", BaseURL: srv.URL, Structured: StructuredPrompt})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := p.Complete(t.Context(), Request{Model: "m", System: "sys", User: "u", Schema: json.RawMessage(decisionSchema)})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != `{"action": "fold"}` {
		t.Errorf("Content = %q, want the extracted balanced JSON object", resp.Content)
	}
}

func TestOpenAI_NoSchema_NoResponseFormat(t *testing.T) {
	for _, mode := range []StructuredMode{StructuredJSONSchema, StructuredJSONObject, StructuredPrompt, ""} {
		mode := mode
		t.Run(string(mode), func(t *testing.T) {
			srv, body, _ := captureServer(t)
			p, err := NewOpenAIProvider(ProviderConfig{Type: "openai", BaseURL: srv.URL, Structured: mode})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := p.Complete(t.Context(), Request{Model: "m", System: "s", User: "u"}); err != nil {
				t.Fatal(err)
			}
			if _, has := (*body)["response_format"]; has {
				t.Errorf("nil Schema should never send response_format, got: %#v", (*body)["response_format"])
			}
		})
	}
}

func TestOpenAI_ReasoningEffortExtra(t *testing.T) {
	srv, body, _ := captureServer(t)
	p, err := NewOpenAIProvider(ProviderConfig{
		Type: "openai", BaseURL: srv.URL,
		Extra: map[string]any{"reasoning_effort": "none"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(t.Context(), Request{Model: "m", System: "s", User: "u"}); err != nil {
		t.Fatal(err)
	}
	if (*body)["reasoning_effort"] != "none" {
		t.Errorf("reasoning_effort = %v, want \"none\" (thinking must stay off)", (*body)["reasoning_effort"])
	}
}

func TestOpenAI_Headers(t *testing.T) {
	srv, _, headers := captureServer(t)
	p, err := NewOpenAIProvider(ProviderConfig{
		Type: "openai", BaseURL: srv.URL,
		Headers: map[string]string{"X-Test-Header": "hello"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(t.Context(), Request{Model: "m", System: "s", User: "u"}); err != nil {
		t.Fatal(err)
	}
	if got := headers.Get("X-Test-Header"); got != "hello" {
		t.Errorf("X-Test-Header = %q, want hello", got)
	}
}

func TestOpenAI_MaxTokens(t *testing.T) {
	srv, body, _ := captureServer(t)
	p, err := NewOpenAIProvider(ProviderConfig{Type: "openai", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(t.Context(), Request{Model: "m", System: "s", User: "u", MaxTokens: 321}); err != nil {
		t.Fatal(err)
	}
	got, ok := (*body)["max_tokens"].(float64)
	if !ok || got != 321 {
		t.Errorf("max_tokens = %v, want 321", (*body)["max_tokens"])
	}
}

func TestOpenAI_MaxTokens_ZeroOmitted(t *testing.T) {
	srv, body, _ := captureServer(t)
	p, err := NewOpenAIProvider(ProviderConfig{Type: "openai", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(t.Context(), Request{Model: "m", System: "s", User: "u"}); err != nil {
		t.Fatal(err)
	}
	if _, has := (*body)["max_tokens"]; has {
		t.Errorf("max_tokens should be omitted when Request.MaxTokens is 0, got: %v", (*body)["max_tokens"])
	}
}

func TestOpenAI_AuthHeader_DummyKeyWhenNoAPIKeyEnv(t *testing.T) {
	srv, _, headers := captureServer(t)
	p, err := NewOpenAIProvider(ProviderConfig{Type: "openai", BaseURL: srv.URL, APIKeyEnv: ""})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(t.Context(), Request{Model: "m", System: "s", User: "u"}); err != nil {
		t.Fatal(err)
	}
	if got := headers.Get("Authorization"); got != "Bearer ollama" {
		t.Errorf("Authorization = %q, want Bearer ollama", got)
	}
}

func TestOpenAI_AuthHeader_FromEnv(t *testing.T) {
	t.Setenv("POKERLLM_TEST_API_KEY", "secret-123")
	srv, _, headers := captureServer(t)
	p, err := NewOpenAIProvider(ProviderConfig{Type: "openai", BaseURL: srv.URL, APIKeyEnv: "POKERLLM_TEST_API_KEY"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.Complete(t.Context(), Request{Model: "m", System: "s", User: "u"}); err != nil {
		t.Fatal(err)
	}
	if got := headers.Get("Authorization"); got != "Bearer secret-123" {
		t.Errorf("Authorization = %q, want Bearer secret-123", got)
	}
}

func TestOpenAI_APIKeyEnv_SetButEmpty_ErrorsAtConstruction(t *testing.T) {
	t.Setenv("POKERLLM_TEST_API_KEY_UNSET", "")
	_, err := NewOpenAIProvider(ProviderConfig{Type: "openai", BaseURL: "http://unused.invalid", APIKeyEnv: "POKERLLM_TEST_API_KEY_UNSET"})
	if err == nil {
		t.Fatal("NewOpenAIProvider error = nil, want an error when api_key_env names an empty/unset var")
	}
}

func TestOpenAI_EmptyContentWithReasoning(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id":"x","object":"chat.completion","created":1,"model":"m",
			"choices":[{"index":0,"finish_reason":"stop","message":{"role":"assistant","content":"","reasoning":"pensando mucho..."}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`))
	}))
	defer srv.Close()

	p, err := NewOpenAIProvider(ProviderConfig{Type: "openai", BaseURL: srv.URL})
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Complete(t.Context(), Request{Model: "m", System: "s", User: "u"})
	if !errors.Is(err, ErrBadOutput) {
		t.Fatalf("Complete() error = %v, want errors.Is(err, ErrBadOutput)", err)
	}
	if !strings.Contains(err.Error(), "reasoning_effort") {
		t.Errorf("error should hint at reasoning_effort: none, got: %v", err)
	}
}
