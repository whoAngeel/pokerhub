// Package player defines the turn contract between the table and anyone who
// acts (LLM agents, rule bots, humans). It is a pure DTO layer: it must not
// import internal/engine, so agents can never reach engine internals.
package player

import (
	"context"
	"time"
)

type LegalAction struct {
	Type   string `json:"type"`
	Amount int    `json:"amount,omitempty"`
	Min    int    `json:"min,omitempty"`
	Max    int    `json:"max,omitempty"`
}

type Action struct {
	Type   string `json:"type"`
	Amount int    `json:"amount"`
}

type Me struct {
	Seat  int      `json:"seat"`
	Name  string   `json:"name"`
	Hole  []string `json:"hole"`
	Stack int      `json:"stack"`
	InPot int      `json:"in_pot"`
}

type Helpers struct {
	MadeHand string  `json:"made_hand"`
	Equity   float64 `json:"equity"`
	PotOdds  float64 `json:"pot_odds"`
}

type Stats struct {
	Hands            int     `json:"hands"`
	VPIP             float64 `json:"vpip"`
	PFR              float64 `json:"pfr"`
	AF               float64 `json:"af"`
	RiverBluffsShown int     `json:"river_bluffs_shown"`
}

type Opponent struct {
	Seat       int      `json:"seat"`
	Name       string   `json:"name"`
	Stack      int      `json:"stack"`
	Status     string   `json:"status"`
	LastAction string   `json:"last_action,omitempty"`
	Stats      *Stats   `json:"stats,omitempty"`
	Notes      []string `json:"notes,omitempty"`
}

type ChatLine struct {
	Seat int    `json:"seat"`
	Say  string `json:"say"`
}

// DecisionView is everything a player may know when acting. Built by the
// table from engine.ViewFor(seat): only own hole cards + public info.
type DecisionView struct {
	HandID       int           `json:"hand"`
	Street       string        `json:"street"`
	Button       int           `json:"button"`
	You          Me            `json:"you"`
	Board        []string      `json:"board"`
	Pot          int           `json:"pot"`
	ToCall       int           `json:"to_call"`
	Helpers      Helpers       `json:"helpers"`
	ActionLog    string        `json:"action_log"`
	Opponents    []Opponent    `json:"opponents"`
	Chat         []ChatLine    `json:"chat"`
	LegalActions []LegalAction `json:"legal_actions"`
}

// Reasoning is PRIVATE: only spectators ever see it.
type Reasoning struct {
	Hand  string `json:"hand"`
	Odds  string `json:"odds"`
	Reads string `json:"reads"`
	Plan  string `json:"plan"`
}

type DecisionMeta struct {
	Provider  string        `json:"provider"`
	Model     string        `json:"model"`
	Latency   time.Duration `json:"-"`
	LatencyMS int64         `json:"latency_ms"`
	TokensOut int           `json:"tokens_out"`
	Retries   int           `json:"retries"`
	Fallback  bool          `json:"fallback"`
	Warnings  []string      `json:"warnings"`
}

type Decision struct {
	Action    Action
	Reasoning *Reasoning // nil for rule bots
	Say       string     // public, <=120 chars, may be empty
	Meta      DecisionMeta
}

// Player decides an action. Implementations must respect ctx and always
// return a Decision (never block the table); the table re-validates it.
type Player interface {
	Name() string
	Kind() string // "llm" | "rulebot" | "human"
	Decide(ctx context.Context, v DecisionView) Decision
}

// Warmer is optionally implemented by players backed by a model: the table
// calls Warmup once before the first hand to absorb the model's cold start.
type Warmer interface {
	Warmup(ctx context.Context) error
}
