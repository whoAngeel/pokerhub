// Package llmtest provides a shared conformance suite for llm.Provider
// implementations whose wire protocol is OpenAI-compatible Chat Completions
// (the provider sends a POST with a chat-completion-shaped JSON body and
// expects a chat-completion-shaped JSON response).
//
// Limitation: a provider whose wire protocol isn't OpenAI-compatible (a
// native Anthropic client, Bedrock, a bespoke server, ...) cannot be
// exercised against the fake server here, because the fake server only
// speaks the OpenAI Chat Completions wire format. Such a provider needs its
// own protocol-level tests instead; see docs/PLAN.md §6.1, Nivel 3.
package llmtest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/whoAngeel/pokerhub/internal/llm"
)

// RunConformance runs the shared provider conformance suite against
// newProvider, which must build a Provider pointed at the given base URL
// (e.g. http://127.0.0.1:NNNN, an OpenAI-compatible /v1-style endpoint).
// Each subtest spins up its own httptest.Server with a handler tailored to
// that scenario, so newProvider is called once per subtest.
//
// Example, from a provider's own test file:
//
//	func TestOpenAIConformance(t *testing.T) {
//		llmtest.RunConformance(t, func(baseURL string) llm.Provider {
//			p, err := llm.NewOpenAIProvider(llm.ProviderConfig{Type: "openai", BaseURL: baseURL})
//			if err != nil {
//				t.Fatal(err)
//			}
//			return p
//		})
//	}
func RunConformance(t *testing.T, newProvider func(baseURL string) llm.Provider) {
	t.Run("success_with_schema", func(t *testing.T) { testSuccessWithSchema(t, newProvider) })
	t.Run("ctx_timeout", func(t *testing.T) { testCtxTimeout(t, newProvider) })
	t.Run("ctx_cancel", func(t *testing.T) { testCtxCancel(t, newProvider) })
	t.Run("rate_limit_429", func(t *testing.T) { testRateLimit429(t, newProvider) })
	t.Run("unavailable_500", func(t *testing.T) { testUnavailable500(t, newProvider) })
	t.Run("empty_content_with_reasoning", func(t *testing.T) { testEmptyContentWithReasoning(t, newProvider) })
	t.Run("garbage_body_never_panics", func(t *testing.T) { testGarbageBodyNeverPanics(t, newProvider) })
}

const testSchema = `{"type":"object","properties":{"action":{"type":"string"}},"required":["action"]}`

func testSuccessWithSchema(t *testing.T, newProvider func(string) llm.Provider) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeChatCompletion(w, `{"action":"check"}`, nil)
	}))
	defer srv.Close()

	p := newProvider(srv.URL)
	resp, err := safeComplete(t, p, context.Background(), llm.Request{
		Model:      "test-model",
		System:     "sys",
		User:       "usr",
		Schema:     json.RawMessage(testSchema),
		SchemaName: "decision",
	})
	if err != nil {
		t.Fatalf("Complete() error = %v, want nil", err)
	}
	if resp.Content == "" {
		t.Fatalf("Complete() returned empty content")
	}
	if resp.Usage.PromptTokens == 0 && resp.Usage.OutputTokens == 0 {
		t.Errorf("Complete() returned zero Usage, want the server's reported token counts")
	}
}

func testCtxTimeout(t *testing.T, newProvider func(string) llm.Provider) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// r.Context() doesn't reliably observe the client giving up over a
		// plain loopback connection within the test's short budget, so cap
		// the fallback low: it only affects how long this handler (and thus
		// srv.Close()'s wait for it) takes, not what's being asserted.
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
			writeChatCompletion(w, `{"action":"check"}`, nil)
		}
	}))
	defer srv.Close()

	p := newProvider(srv.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := safeComplete(t, p, ctx, llm.Request{Model: "m", System: "s", User: "u"})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatalf("Complete() error = nil, want a timeout error")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Complete() took %v, want it to return promptly on ctx timeout", elapsed)
	}
}

func testCtxCancel(t *testing.T, newProvider func(string) llm.Provider) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(300 * time.Millisecond):
			writeChatCompletion(w, `{"action":"check"}`, nil)
		}
	}))
	defer srv.Close()

	p := newProvider(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, err := safeComplete(t, p, ctx, llm.Request{Model: "m", System: "s", User: "u"})
		errCh <- err
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatalf("Complete() error = nil, want a cancellation error")
		}
	case <-time.After(4 * time.Second):
		t.Fatalf("Complete() did not return after ctx cancellation")
	}
}

func testRateLimit429(t *testing.T, newProvider func(string) llm.Provider) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"rate limited","type":"rate_limit_error"}}`))
	}))
	defer srv.Close()

	p := newProvider(srv.URL)
	_, err := safeComplete(t, p, context.Background(), llm.Request{Model: "m", System: "s", User: "u"})
	if !errors.Is(err, llm.ErrRateLimit) {
		t.Fatalf("Complete() error = %v, want errors.Is(err, llm.ErrRateLimit)", err)
	}
}

func testUnavailable500(t *testing.T, newProvider func(string) llm.Provider) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom","type":"server_error"}}`))
	}))
	defer srv.Close()

	p := newProvider(srv.URL)
	_, err := safeComplete(t, p, context.Background(), llm.Request{Model: "m", System: "s", User: "u"})
	if !errors.Is(err, llm.ErrUnavailable) {
		t.Fatalf("Complete() error = %v, want errors.Is(err, llm.ErrUnavailable)", err)
	}
}

func testEmptyContentWithReasoning(t *testing.T, newProvider func(string) llm.Provider) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeChatCompletionRaw(w, `{
			"id":"x","object":"chat.completion","created":1,"model":"m",
			"choices":[{"index":0,"finish_reason":"stop","message":{
				"role":"assistant","content":"","reasoning":"thinking it over..."
			}}],
			"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}
		}`)
	}))
	defer srv.Close()

	p := newProvider(srv.URL)
	_, err := safeComplete(t, p, context.Background(), llm.Request{Model: "m", System: "s", User: "u"})
	if !errors.Is(err, llm.ErrBadOutput) {
		t.Fatalf("Complete() error = %v, want errors.Is(err, llm.ErrBadOutput)", err)
	}
}

func testGarbageBodyNeverPanics(t *testing.T, newProvider func(string) llm.Provider) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`not json at all {{{`))
	}))
	defer srv.Close()

	p := newProvider(srv.URL)
	_, err := safeComplete(t, p, context.Background(), llm.Request{Model: "m", System: "s", User: "u"})
	if err == nil {
		t.Fatalf("Complete() error = nil, want an error for a garbage response body")
	}
}

// safeComplete calls p.Complete and turns any panic into a clean test
// failure instead of crashing the whole test binary, since "never panics on
// garbage body" is exactly what this suite is checking.
func safeComplete(t *testing.T, p llm.Provider, ctx context.Context, req llm.Request) (resp llm.Response, err error) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Provider.Complete panicked: %v", r)
		}
	}()
	return p.Complete(ctx, req)
}

func writeChatCompletion(w http.ResponseWriter, content string, extra map[string]any) {
	body := map[string]any{
		"id":      "test-id",
		"object":  "chat.completion",
		"created": 1,
		"model":   "test-model",
		"choices": []map[string]any{
			{
				"index":         0,
				"finish_reason": "stop",
				"message": map[string]any{
					"role":    "assistant",
					"content": content,
				},
			},
		},
		"usage": map[string]any{
			"prompt_tokens":     10,
			"completion_tokens": 5,
			"total_tokens":      15,
		},
	}
	for k, v := range extra {
		body[k] = v
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(body)
}

func writeChatCompletionRaw(w http.ResponseWriter, raw string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(raw))
}
