package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/atzomx/pokerllm/internal/llm"
	"github.com/atzomx/pokerllm/internal/player"
)

func agentTestPersona() Persona {
	return Persona{
		ID: "profesor", Name: "El Profesor", Temperature: 0.4,
		Reasoning: ReasoningRich, Chattiness: 0.5,
		Voice: "Pedante y analítico.", Strategy: "Decide con números.",
	}
}

func agentTestView(legal ...player.LegalAction) player.DecisionView {
	return player.DecisionView{
		HandID: 1, Street: "flop",
		You:          player.Me{Seat: 0, Hole: []string{"Ah", "Kd"}, Stack: 880},
		Board:        []string{"Ks", "7h", "2c"},
		Pot:          100,
		ToCall:       0,
		LegalActions: legal,
	}
}

const validContent = `{"reasoning":{"hand":"h","odds":"o","reads":"r","plan":"p"},"action":"check","amount":0,"say":""}`

func TestLLMAgent_NameAndKind(t *testing.T) {
	a := New("Mesa 1", agentTestPersona(), llm.NewMock(), "mock", "m1")
	if a.Name() != "Mesa 1" {
		t.Errorf("Name() = %q, want %q", a.Name(), "Mesa 1")
	}
	if a.Kind() != "llm" {
		t.Errorf("Kind() = %q, want llm", a.Kind())
	}
}

func TestLLMAgent_Decide_SuccessOnFirstTry(t *testing.T) {
	mock := llm.NewMock(llm.MockResponse{Content: validContent})
	a := New("Mesa 1", agentTestPersona(), mock, "local", "gemma4:e4b")
	v := agentTestView(player.LegalAction{Type: "check"})

	d := a.Decide(context.Background(), v)

	if d.Action.Type != "check" {
		t.Errorf("Action.Type = %q, want check", d.Action.Type)
	}
	if d.Meta.Fallback {
		t.Errorf("Meta.Fallback = true, want false")
	}
	if d.Meta.Retries != 0 {
		t.Errorf("Meta.Retries = %d, want 0", d.Meta.Retries)
	}
	if d.Meta.Provider != "local" || d.Meta.Model != "gemma4:e4b" {
		t.Errorf("Meta.Provider/Model = %q/%q, want local/gemma4:e4b", d.Meta.Provider, d.Meta.Model)
	}
	if d.Meta.LatencyMS < 0 {
		t.Errorf("Meta.LatencyMS = %d, want >= 0", d.Meta.LatencyMS)
	}
	if len(mock.Requests()) != 1 {
		t.Fatalf("provider called %d times, want 1", len(mock.Requests()))
	}
}

func TestLLMAgent_Decide_RequestCarriesScheamaTokensAndTemperature(t *testing.T) {
	mock := llm.NewMock(llm.MockResponse{Content: validContent})
	p := agentTestPersona()
	p.Reasoning = ReasoningShort
	p.Temperature = 0.77
	a := New("Mesa 1", p, mock, "local", "gemma4:e4b")
	v := agentTestView(player.LegalAction{Type: "check"})

	_ = a.Decide(context.Background(), v)

	reqs := mock.Requests()
	if len(reqs) != 1 {
		t.Fatalf("provider called %d times, want 1", len(reqs))
	}
	req := reqs[0]
	if string(req.Schema) != string(DecisionSchema) {
		t.Errorf("Request.Schema not propagated")
	}
	if req.SchemaName != SchemaName {
		t.Errorf("Request.SchemaName = %q, want %q", req.SchemaName, SchemaName)
	}
	if req.MaxTokens != 180 { // short reasoning
		t.Errorf("Request.MaxTokens = %d, want 180 for short reasoning", req.MaxTokens)
	}
	if req.Temperature != 0.77 {
		t.Errorf("Request.Temperature = %v, want 0.77", req.Temperature)
	}
	if req.Model != "gemma4:e4b" {
		t.Errorf("Request.Model = %q, want gemma4:e4b", req.Model)
	}
	if !strings.Contains(req.System, "español") {
		t.Errorf("Request.System doesn't look like the Spanish system prompt")
	}
}

func TestLLMAgent_Decide_BadJSONThenSuccess_Retries(t *testing.T) {
	mock := llm.NewMock(
		llm.MockResponse{Content: "not json at all"},
		llm.MockResponse{Content: validContent},
	)
	a := New("Mesa 1", agentTestPersona(), mock, "local", "gemma4:e4b")
	v := agentTestView(player.LegalAction{Type: "check"}, player.LegalAction{Type: "raise", Min: 10, Max: 880})

	d := a.Decide(context.Background(), v)

	if d.Meta.Fallback {
		t.Fatalf("Meta.Fallback = true, want false (should have recovered on retry)")
	}
	if d.Meta.Retries != 1 {
		t.Errorf("Meta.Retries = %d, want 1", d.Meta.Retries)
	}
	if d.Action.Type != "check" {
		t.Errorf("Action.Type = %q, want check", d.Action.Type)
	}

	reqs := mock.Requests()
	if len(reqs) != 2 {
		t.Fatalf("provider called %d times, want 2", len(reqs))
	}
	retryUserText := reqs[1].User
	if !strings.Contains(retryUserText, "check") || !strings.Contains(retryUserText, "raise entre 10 y 880") {
		t.Errorf("retry prompt doesn't list legal actions: %q", retryUserText)
	}
	if !strings.Contains(strings.ToLower(retryUserText), "no fue válida") {
		t.Errorf("retry prompt should explain the previous answer was invalid: %q", retryUserText)
	}
}

func TestLLMAgent_Decide_BadTwice_FallbackChecksWhenLegal(t *testing.T) {
	mock := llm.NewMock(
		llm.MockResponse{Content: "still not json"},
		llm.MockResponse{Content: "still not json"},
	)
	a := New("Mesa 1", agentTestPersona(), mock, "local", "gemma4:e4b")
	v := agentTestView(player.LegalAction{Type: "check"}, player.LegalAction{Type: "raise", Min: 10, Max: 880})

	d := a.Decide(context.Background(), v)

	if !d.Meta.Fallback {
		t.Fatal("Meta.Fallback = false, want true")
	}
	if d.Action.Type != "check" {
		t.Errorf("Action.Type = %q, want check (it's legal)", d.Action.Type)
	}
	if d.Action.Amount != 0 {
		t.Errorf("Action.Amount = %d, want 0", d.Action.Amount)
	}
	if d.Reasoning != nil {
		t.Errorf("Reasoning = %+v, want nil on fallback", d.Reasoning)
	}
	if len(mock.Requests()) != 2 {
		t.Fatalf("provider called %d times, want 2 (1 initial + 1 retry)", len(mock.Requests()))
	}
}

func TestLLMAgent_Decide_BadTwice_FallbackFoldsWhenCheckIllegal(t *testing.T) {
	mock := llm.NewMock(
		llm.MockResponse{Content: "nope"},
		llm.MockResponse{Content: "nope"},
	)
	a := New("Mesa 1", agentTestPersona(), mock, "local", "gemma4:e4b")
	v := agentTestView(player.LegalAction{Type: "fold"}, player.LegalAction{Type: "call", Amount: 60})

	d := a.Decide(context.Background(), v)

	if !d.Meta.Fallback {
		t.Fatal("Meta.Fallback = false, want true")
	}
	if d.Action.Type != "fold" {
		t.Errorf("Action.Type = %q, want fold (check isn't legal)", d.Action.Type)
	}
}

func TestLLMAgent_Decide_RateLimit_Retries(t *testing.T) {
	mock := llm.NewMock(
		llm.MockResponse{Err: llm.ErrRateLimit},
		llm.MockResponse{Content: validContent},
	)
	a := New("Mesa 1", agentTestPersona(), mock, "local", "gemma4:e4b")
	v := agentTestView(player.LegalAction{Type: "check"})

	start := time.Now()
	d := a.Decide(context.Background(), v)
	elapsed := time.Since(start)

	if d.Meta.Fallback {
		t.Fatalf("Meta.Fallback = true, want false (should recover after the rate-limit retry)")
	}
	if d.Meta.Retries != 1 {
		t.Errorf("Meta.Retries = %d, want 1", d.Meta.Retries)
	}
	if elapsed < retryBackoff {
		t.Errorf("elapsed = %v, want >= retryBackoff (%v): backoff should have been respected", elapsed, retryBackoff)
	}
	if len(mock.Requests()) != 2 {
		t.Fatalf("provider called %d times, want 2", len(mock.Requests()))
	}
}

func TestLLMAgent_Decide_RateLimitTwice_Fallback(t *testing.T) {
	mock := llm.NewMock(
		llm.MockResponse{Err: llm.ErrRateLimit},
		llm.MockResponse{Err: llm.ErrRateLimit},
	)
	a := New("Mesa 1", agentTestPersona(), mock, "local", "gemma4:e4b")
	v := agentTestView(player.LegalAction{Type: "check"})

	d := a.Decide(context.Background(), v)

	if !d.Meta.Fallback {
		t.Fatal("Meta.Fallback = false, want true")
	}
	if !containsString(d.Meta.Warnings, "fallback:rate_limit") {
		t.Errorf("Warnings = %v, want fallback:rate_limit", d.Meta.Warnings)
	}
}

func TestLLMAgent_Decide_Unavailable_FallbackImmediately(t *testing.T) {
	mock := llm.NewMock(llm.MockResponse{Err: llm.ErrUnavailable})
	a := New("Mesa 1", agentTestPersona(), mock, "local", "gemma4:e4b")
	v := agentTestView(player.LegalAction{Type: "check"})

	d := a.Decide(context.Background(), v)

	if !d.Meta.Fallback {
		t.Fatal("Meta.Fallback = false, want true")
	}
	if len(mock.Requests()) != 1 {
		t.Errorf("provider called %d times, want 1 (ErrUnavailable should not retry)", len(mock.Requests()))
	}
	if !containsString(d.Meta.Warnings, "fallback:provider_unavailable") {
		t.Errorf("Warnings = %v, want fallback:provider_unavailable", d.Meta.Warnings)
	}
}

func TestLLMAgent_Decide_Timeout_Fallback(t *testing.T) {
	mock := llm.NewMock(llm.MockResponse{Content: validContent, Delay: 200 * time.Millisecond})
	a := New("Mesa 1", agentTestPersona(), mock, "local", "gemma4:e4b")
	v := agentTestView(player.LegalAction{Type: "check"})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	d := a.Decide(ctx, v)
	elapsed := time.Since(start)

	if !d.Meta.Fallback {
		t.Fatal("Meta.Fallback = false, want true")
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("elapsed = %v, want a prompt return on ctx timeout, not the full 1 retry + backoff", elapsed)
	}
	if !containsString(d.Meta.Warnings, "fallback:timeout") {
		t.Errorf("Warnings = %v, want fallback:timeout", d.Meta.Warnings)
	}
}

func TestLLMAgent_Decide_CtxAlreadyCanceled_Fallback(t *testing.T) {
	mock := llm.NewMock(llm.MockResponse{Content: validContent})
	a := New("Mesa 1", agentTestPersona(), mock, "local", "gemma4:e4b")
	v := agentTestView(player.LegalAction{Type: "check"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	d := a.Decide(ctx, v)

	if !d.Meta.Fallback {
		t.Fatal("Meta.Fallback = false, want true")
	}
	if len(mock.Requests()) != 0 {
		t.Errorf("provider called %d times, want 0 (ctx was already done)", len(mock.Requests()))
	}
}

func TestLLMAgent_Decide_NeverPanics(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Decide panicked: %v", r)
		}
	}()
	mock := llm.NewMock(llm.MockResponse{Content: "{garbage"})
	a := New("Mesa 1", agentTestPersona(), mock, "local", "gemma4:e4b")
	v := agentTestView() // no legal actions at all: exercises the fold fallback path
	_ = a.Decide(context.Background(), v)
}
