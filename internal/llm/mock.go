package llm

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

func init() {
	Register("mock", func(cfg ProviderConfig) (Provider, error) {
		return NewMock(), nil
	})
}

// MockResponse is one scripted reply for MockProvider: either Content or Err
// (not both), with an optional Delay that respects ctx cancellation.
type MockResponse struct {
	Content string
	Err     error
	Delay   time.Duration
}

// MockProvider is a Provider for tests: no network, no GPU. It can be driven
// in two ways:
//   - a scripted queue of MockResponse (NewMock), consumed in order;
//   - an arbitrary function of the request (MockFunc).
//
// It records every Request it receives (thread-safe) so tests can assert on
// what was sent.
type MockProvider struct {
	mu       sync.Mutex
	queue    []MockResponse
	fn       func(Request) (Response, error)
	requests []Request
}

// NewMock returns a MockProvider that serves responses from the queue in
// order. Once the queue is empty, it falls back to a deterministic valid
// JSON response (see Complete).
func NewMock(responses ...MockResponse) *MockProvider {
	return &MockProvider{queue: append([]MockResponse{}, responses...)}
}

// MockFunc returns a MockProvider backed by fn: every Complete call is
// forwarded to fn instead of consuming a scripted queue.
func MockFunc(fn func(Request) (Response, error)) *MockProvider {
	return &MockProvider{fn: fn}
}

func (p *MockProvider) Name() string { return "mock" }

// Requests returns a copy of every Request received so far, in order.
func (p *MockProvider) Requests() []Request {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]Request, len(p.requests))
	copy(out, p.requests)
	return out
}

func (p *MockProvider) Complete(ctx context.Context, req Request) (Response, error) {
	p.mu.Lock()
	p.requests = append(p.requests, req)
	fn := p.fn
	var next *MockResponse
	if fn == nil && len(p.queue) > 0 {
		mr := p.queue[0]
		p.queue = p.queue[1:]
		next = &mr
	}
	p.mu.Unlock()

	start := time.Now()

	if fn != nil {
		return fn(req)
	}

	if next != nil {
		if next.Delay > 0 {
			select {
			case <-time.After(next.Delay):
			case <-ctx.Done():
				return Response{}, ctx.Err()
			}
		}
		if next.Err != nil {
			return Response{}, next.Err
		}
		return Response{Content: next.Content, Latency: time.Since(start)}, nil
	}

	return Response{Content: defaultMockContent(req.Schema), Latency: time.Since(start)}, nil
}

// defaultMockContent returns a deterministic, always-valid JSON body: a full
// poker decision shape when the schema declares an "action" property (the
// agent layer's decision schema), or "{}" otherwise.
func defaultMockContent(schema json.RawMessage) string {
	if hasActionProperty(schema) {
		return `{"reasoning":{"hand":"mock","odds":"mock","reads":"mock","plan":"mock"},"action":"check","amount":0,"say":""}`
	}
	return "{}"
}

func hasActionProperty(schema json.RawMessage) bool {
	if len(schema) == 0 {
		return false
	}
	var parsed struct {
		Properties map[string]any `json:"properties"`
	}
	if err := json.Unmarshal(schema, &parsed); err != nil {
		return false
	}
	_, ok := parsed.Properties["action"]
	return ok
}
