package protocol

// This file holds one payload struct per event/message named in
// docs/PROTOCOL.md §4 (server->client) and §5 (client->server). Field names
// and JSON tags match the examples there exactly; see GenerateSchemas for
// the JSON Schema derived from these same structs.

// --- server -> client (§4) ---

// SnapshotPayload is the "snapshot" event: the full table state for the
// connecting role. HandID is nil before the first hand starts. ToAct is -1
// when nobody is to act. LegalActions is present only when ToAct names a
// seat the connected role may see (spectator, or that seat's own player) --
// server.Project enforces this; TableState.Snapshot always fills it in.
type SnapshotPayload struct {
	HandID       *int           `json:"hand_id"`
	Street       string         `json:"street"`
	Button       int            `json:"button"`
	SmallBlind   int            `json:"small_blind"`
	BigBlind     int            `json:"big_blind"`
	Board        []Card         `json:"board"`
	Pot          int            `json:"pot"`
	ToAct        int            `json:"to_act"`
	Seats        []SnapshotSeat `json:"seats"`
	LegalActions []LegalAction  `json:"legal_actions,omitempty"`
}

// HandStartSeat is the reduced seat shape inside hand_start (seat/name/stack
// only, per PROTOCOL.md §4's example).
type HandStartSeat struct {
	Seat  int    `json:"seat"`
	Name  string `json:"name"`
	Stack int    `json:"stack"`
}

// HandStartPayload is the "hand_start" event: public.
type HandStartPayload struct {
	HandID     int             `json:"hand_id"`
	Button     int             `json:"button"`
	SmallBlind int             `json:"small_blind"`
	BigBlind   int             `json:"big_blind"`
	Seats      []HandStartSeat `json:"seats"`
}

// BlindPayload is the "blind" event: public, one per posted blind.
type BlindPayload struct {
	Seat       int    `json:"seat"`
	Type       string `json:"type"` // "small" | "big"
	Amount     int    `json:"amount"`
	StackAfter int    `json:"stack_after"`
	PotAfter   int    `json:"pot_after"`
}

// HoleCardsPayload is the "hole_cards" event: owner seat + spectator only.
type HoleCardsPayload struct {
	Seat  int    `json:"seat"`
	Cards []Card `json:"cards"`
}

// StreetPayload is the "street" event: public.
type StreetPayload struct {
	Street string `json:"street"` // "preflop" | "flop" | "turn" | "river" | "showdown"
	Board  []Card `json:"board"`
}

// TurnPayload is the "turn" event: public, but LegalActions is present only
// for the owner seat and spectator (server.Project strips it otherwise).
type TurnPayload struct {
	Seat         int           `json:"seat"`
	ToCall       int           `json:"to_call"`
	DeadlineMS   int           `json:"deadline_ms"`
	LegalActions []LegalAction `json:"legal_actions,omitempty"`
}

// ThinkingPayload is the "thinking" event: public, no extra data beyond the
// seat (UI spinner cue).
type ThinkingPayload struct {
	Seat int `json:"seat"`
}

// ActionPayload is the "action" event (server->client): public. Amount is
// what this action ADDS to the pot, not the street total.
type ActionPayload struct {
	Seat       int    `json:"seat"`
	Type       string `json:"type"`
	Amount     int    `json:"amount"`
	StackAfter int    `json:"stack_after"`
	PotAfter   int    `json:"pot_after"`
}

// ChatPayload is the "chat" event: public, <=120 chars (clamped server-side).
type ChatPayload struct {
	Seat int    `json:"seat"`
	Say  string `json:"say"`
}

// ReasoningPayload is the "reasoning" event: spectator ONLY, never delivered
// to any player, not even the seat it's about.
type ReasoningPayload struct {
	Seat  int    `json:"seat"`
	Hand  string `json:"hand"`
	Odds  string `json:"odds"`
	Reads string `json:"reads"`
	Plan  string `json:"plan"`
	Meta  Meta   `json:"meta"`
}

// Reveal is one seat's showdown reveal.
type Reveal struct {
	Seat  int    `json:"seat"`
	Cards []Card `json:"cards"`
	Rank  string `json:"rank"`
}

// ShowdownPayload is the "showdown" event: public (reveals are public by the
// rules of the game once showdown happens).
type ShowdownPayload struct {
	Reveals []Reveal `json:"reveals"`
}

// Pot is one main/side pot, with winners filled in on award.
type Pot struct {
	Amount  int   `json:"amount"`
	Winners []int `json:"winners"`
}

// PotAwardedPayload is the "pot_awarded" event: public.
type PotAwardedPayload struct {
	Pots []Pot `json:"pots"`
}

// HandEndSeat is the reduced seat shape inside hand_end (seat/stack only).
type HandEndSeat struct {
	Seat  int `json:"seat"`
	Stack int `json:"stack"`
}

// HandEndPayload is the "hand_end" event: public.
type HandEndPayload struct {
	HandID int           `json:"hand_id"`
	Seats  []HandEndSeat `json:"seats"`
}

// RebuyPayload is the "rebuy" event: public.
type RebuyPayload struct {
	Seat       int `json:"seat"`
	Amount     int `json:"amount"`
	StackAfter int `json:"stack_after"`
}

// MemoryUpdateEntry is one agent's new notes about its rivals.
type MemoryUpdateEntry struct {
	Seat  int      `json:"seat"`
	Notes []string `json:"notes"`
}

// MemoryUpdatePayload is the "memory_update" event: spectator ONLY. Emitted
// by internal/table (see memory.go) after every hand that reached showdown,
// once per LLM seat whose private notes about a rival just changed (PLAN
// §7). Which agent observed is intentionally not part of this wire shape.
type MemoryUpdatePayload struct {
	Updates []MemoryUpdateEntry `json:"updates"`
}

// AgentErrorPayload is the "agent_error" event: spectator ONLY. Fallback is
// always true: this event only fires when the table actually had to
// override or fall back a decision (see internal/protocol/convert.go).
type AgentErrorPayload struct {
	Seat     int      `json:"seat"`
	Error    string   `json:"error"`
	Fallback bool     `json:"fallback"`
	Warnings []string `json:"warnings"`
}

// --- client -> server (§5) ---

// HelloPayload: sent by anyone on connect/reconnect.
type HelloPayload struct {
	Role    string `json:"role"` // "spectator" | "player"
	Seat    *int   `json:"seat,omitempty"`
	LastSeq *int64 `json:"last_seq,omitempty"`
}

// ControlPayload: spectator-only table pacing control.
type ControlPayload struct {
	Cmd   string   `json:"cmd"` // "pause" | "resume" | "step" | "speed"
	Value *float64 `json:"value,omitempty"`
}

// ClientActionPayload: reserved for the human seat (post-MVP). The server
// always replies with an "error" message ("not supported yet") in the MVP.
type ClientActionPayload struct {
	Type   string `json:"type"`
	Amount *int   `json:"amount,omitempty"`
}

// --- server -> client, beyond §4 (see TypeError's doc comment) ---

// ErrorPayload is the "error" message's payload: a short, human-readable
// reason the server couldn't act on the client's last message.
type ErrorPayload struct {
	Message string `json:"message"`
}
