package protocol

// EventType / message type name constants, one per row of PROTOCOL.md §4
// (server->client) and §5 (client->server). Kept as plain strings (not a
// named type) because they live in Envelope.Type, which must round-trip
// through JSON untouched for an unknown future type (PROTOCOL.md §8:
// additive changes, like a brand new type, must not break an old client).
const (
	TypeSnapshot     = "snapshot"
	TypeHandStart    = "hand_start"
	TypeBlind        = "blind"
	TypeHoleCards    = "hole_cards"
	TypeStreet       = "street"
	TypeTurn         = "turn"
	TypeThinking     = "thinking"
	TypeAction       = "action"
	TypeChat         = "chat"
	TypeReasoning    = "reasoning"
	TypeShowdown     = "showdown"
	TypePotAwarded   = "pot_awarded"
	TypeHandEnd      = "hand_end"
	TypeRebuy        = "rebuy"
	TypeMemoryUpdate = "memory_update"
	TypeAgentError   = "agent_error"

	// client -> server
	TypeHello   = "hello"
	TypeControl = "control"

	// TypeError ("error") is a small server->client addition beyond
	// PROTOCOL.md's §4 table: the server's only reply to a reserved
	// client "action" message in the MVP (docs/PROTOCOL.md §5: "un action
	// ilegal se descarta con un mensaje de error (fuera del alcance de
	// este documento en el MVP)"). Additive per §8 (a client that doesn't
	// know "error" can safely ignore it), so it doesn't bump v. See
	// PROTOCOL.md's "Desviaciones" section for the one-line addition.
	TypeError = "error"
	// TypeAction ("action") doubles as the client->server message name
	// too (PROTOCOL.md §5): same wire name, different payload shape
	// (ClientActionPayload vs ActionPayload), disambiguated by direction.
)

// Card is a 2-character string: rank (23456789TJQKA) + suit (shdc), e.g.
// "Ah", "Td" (PROTOCOL.md §3).
type Card string

// Seat describes a player at the table, with or without a hand in progress
// (PROTOCOL.md §3). PersonaID and Model are nil for rulebot/human seats.
type Seat struct {
	Seat          int     `json:"seat"`
	Name          string  `json:"name"`
	PersonaID     *string `json:"persona_id"`
	Kind          string  `json:"kind"` // "llm" | "rulebot" | "human"
	Stack         int     `json:"stack"`
	Status        string  `json:"status"` // "active" | "folded" | "all_in" | "sitting_out"
	BetThisStreet int     `json:"bet_this_street"`
	Model         *string `json:"model"`
}

// SnapshotSeat is Seat's fields plus the hole-card field that only ever
// appears inside a snapshot payload (PROTOCOL.md §4 "snapshot"): Hole is nil
// unless the connected role may see it. server.Project does the filtering;
// TableState always keeps the god-view (every known hole card) here.
//
// Fields are duplicated from Seat rather than embedded: an anonymous "Seat"
// embed would shadow its own int Seat field (s.Seat would be the embedded
// struct, not the seat number), which is exactly the kind of foot-gun this
// package exists to avoid for every caller that reads a snapshot.
type SnapshotSeat struct {
	Seat          int     `json:"seat"`
	Name          string  `json:"name"`
	PersonaID     *string `json:"persona_id"`
	Kind          string  `json:"kind"`
	Stack         int     `json:"stack"`
	Status        string  `json:"status"`
	BetThisStreet int     `json:"bet_this_street"`
	Model         *string `json:"model"`
	Hole          []Card  `json:"hole"`
}

// LegalAction describes one allowed action (PROTOCOL.md §3). min/max are
// always the resulting TOTAL bet for the street ("raise to"), not the
// increment, so a client can use them directly as slider endpoints.
type LegalAction struct {
	Type   string `json:"type"`
	Amount int    `json:"amount,omitempty"`
	Min    int    `json:"min,omitempty"`
	Max    int    `json:"max,omitempty"`
}

// Meta mirrors the "meta" object embedded in a reasoning payload
// (PROTOCOL.md §4 "reasoning").
type Meta struct {
	Provider  string   `json:"provider"`
	Model     string   `json:"model"`
	LatencyMS int64    `json:"latency_ms"`
	TokensOut int      `json:"tokens_out"`
	Retries   int      `json:"retries"`
	Fallback  bool     `json:"fallback"`
	Warnings  []string `json:"warnings"`
}
