package agent

import (
	"context"
	"errors"
	"time"

	"github.com/atzomx/pokerllm/internal/llm"
	"github.com/atzomx/pokerllm/internal/player"
)

// retryBackoff is the brief pause before the single retry when the provider
// reports llm.ErrRateLimit (PLAN §4.3 / §6.1: "el agente aplica backoff y
// reintenta 1 vez").
const retryBackoff = 300 * time.Millisecond

// maxAttempts is 1 initial call + exactly 1 retry (PLAN §4.3 point 4).
const maxAttempts = 2

// LLMAgent is a player.Player backed by an llm.Provider: it builds the
// prompt+schema from a Persona, asks the provider, and parses/normalizes the
// result (agent/decision.go). It never imports internal/engine.
type LLMAgent struct {
	name         string
	persona      Persona
	provider     llm.Provider
	providerName string
	model        string
}

// New builds an LLMAgent. name is the table-facing display name (may differ
// from persona.Name); providerName/model are recorded in Decision.Meta.
func New(name string, p Persona, prov llm.Provider, providerName, model string) *LLMAgent {
	return &LLMAgent{name: name, persona: p, provider: prov, providerName: providerName, model: model}
}

func (a *LLMAgent) Name() string { return a.name }
func (a *LLMAgent) Kind() string { return "llm" }

// Decide implements player.Player. It never panics and always returns a
// Decision: on any unrecoverable failure (second parse failure, timeout, ctx
// cancellation, or llm.ErrUnavailable) it falls back to check-if-legal-else-
// fold with Meta.Fallback = true.
func (a *LLMAgent) Decide(ctx context.Context, v player.DecisionView) player.Decision {
	start := time.Now()
	req := a.buildRequest(v)

	var retries, tokensOut int

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return a.fallback(v, start, retries, tokensOut, "fallback:ctx_done")
		}

		resp, err := a.provider.Complete(ctx, req)
		if err != nil {
			switch {
			case errors.Is(err, llm.ErrRateLimit):
				if attempt >= maxAttempts {
					return a.fallback(v, start, retries, tokensOut, "fallback:rate_limit")
				}
				if !sleepRespectingCtx(ctx, retryBackoff) {
					return a.fallback(v, start, retries, tokensOut, "fallback:ctx_done")
				}
				retries++
				req.User = a.retryUser(v, "el proveedor está saturado (rate limit); inténtalo de nuevo")
				continue

			case errors.Is(err, llm.ErrUnavailable):
				return a.fallback(v, start, retries, tokensOut, "fallback:provider_unavailable")

			case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
				return a.fallback(v, start, retries, tokensOut, "fallback:timeout")

			case attempt < maxAttempts:
				retries++
				req.User = a.retryUser(v, err.Error())
				continue

			default:
				return a.fallback(v, start, retries, tokensOut, "fallback:provider_error")
			}
		}

		tokensOut += resp.Usage.OutputTokens

		decision, perr := ParseAndNormalize(resp.Content, v)
		if perr == nil {
			return a.finish(decision, start, retries, tokensOut)
		}

		if attempt < maxAttempts {
			retries++
			req.User = a.retryUser(v, perr.Error())
			continue
		}
		return a.fallback(v, start, retries, tokensOut, "fallback:parse_error")
	}

	// Unreachable (the loop above always returns), kept so the compiler
	// doesn't require a trailing return and so a future refactor can't
	// silently fall through without a Decision.
	return a.fallback(v, start, retries, tokensOut, "fallback:unreachable")
}

func (a *LLMAgent) buildRequest(v player.DecisionView) llm.Request {
	return llm.Request{
		Model:       a.model,
		System:      SystemPrompt(a.persona),
		User:        UserPrompt(v),
		Schema:      DecisionSchema,
		SchemaName:  SchemaName,
		MaxTokens:   a.persona.MaxTokens(),
		Temperature: a.persona.Temperature,
	}
}

// retryUser appends an explanatory extra turn (in Spanish) to the original
// prompt, naming the problem and listing legal actions (PLAN §4.3 point 4).
func (a *LLMAgent) retryUser(v player.DecisionView, reason string) string {
	return UserPrompt(v) +
		"\n\nTu respuesta anterior no fue válida (" + reason + "). " +
		"Elige una de estas acciones legales: " + legalActionsSpanish(v.LegalActions) + ". " +
		"Responde SOLO con un JSON válido según el esquema."
}

// finish fills Meta on a successfully parsed Decision.
func (a *LLMAgent) finish(d player.Decision, start time.Time, retries, tokensOut int) player.Decision {
	latency := time.Since(start)
	d.Meta.Provider = a.providerName
	d.Meta.Model = a.model
	d.Meta.Latency = latency
	d.Meta.LatencyMS = latency.Milliseconds()
	d.Meta.TokensOut = tokensOut
	d.Meta.Retries = retries
	d.Meta.Fallback = false
	return d
}

// fallback returns check-if-legal-else-fold, marked Meta.Fallback = true
// with cause appended to Warnings (PLAN §4.3 point 5).
func (a *LLMAgent) fallback(v player.DecisionView, start time.Time, retries, tokensOut int, cause string) player.Decision {
	action := player.Action{Type: "fold", Amount: 0}
	for _, la := range v.LegalActions {
		if la.Type == "check" {
			action = player.Action{Type: "check", Amount: 0}
			break
		}
	}
	latency := time.Since(start)
	return player.Decision{
		Action:    action,
		Reasoning: nil,
		Say:       "",
		Meta: player.DecisionMeta{
			Provider:  a.providerName,
			Model:     a.model,
			Latency:   latency,
			LatencyMS: latency.Milliseconds(),
			TokensOut: tokensOut,
			Retries:   retries,
			Fallback:  true,
			Warnings:  []string{cause},
		},
	}
}

// sleepRespectingCtx waits for d, returning false early (without having
// slept the full duration) if ctx is done first.
func sleepRespectingCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
