// Package protocol is the source of truth for the WebSocket wire contract
// between pokerd and any client (docs/PROTOCOL.md). It defines the envelope
// every message travels in, one payload struct per event/message named in
// PROTOCOL.md §4/§5, a Converter that turns table.Event into god-view
// Envelopes, and a TableState reducer that keeps enough state to answer
// "what does the table look like right now" (used to build a snapshot for a
// new or reconnecting connection, and by replay).
//
// protocol intentionally still imports internal/table (Convert's input) and,
// transitively, internal/player/internal/engine: it is the translation layer
// between the trusted orchestrator's internal event shape and the versioned
// JSON shape PROTOCOL.md promises external clients. Nothing downstream of
// protocol (server, replay) needs to import table/player/engine itself.
package protocol

import (
	"encoding/json"
	"fmt"
	"time"
)

// ProtocolVersion is the "v" field of every Envelope (PROTOCOL.md §2).
const ProtocolVersion = 1

// tsLayout renders ts as RFC 3339 UTC with millisecond precision, e.g.
// "2026-10-31T18:04:12.500Z" (PROTOCOL.md §2).
const tsLayout = "2006-01-02T15:04:05.000Z"

// Now returns the current time formatted as PROTOCOL.md §2 expects. The hub
// calls this once per outgoing Envelope; tests may override by constructing
// Envelopes directly instead of going through NewEnvelope.
func Now() string { return time.Now().UTC().Format(tsLayout) }

// ParseTS parses a ts string as Now() formats it (also accepts plain
// RFC3339 for robustness, e.g. a hand-built test fixture without
// millisecond padding). Used by replay.Player to pace playback from the
// gaps between recorded envelopes.
func ParseTS(ts string) (time.Time, error) {
	if t, err := time.Parse(tsLayout, ts); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, ts)
}

// Envelope is the fixed wrapper every message travels in, in either
// direction (PROTOCOL.md §2). Seq and TS are assigned by the hub for
// server->client messages; a client does not set them on hello/control/action
// and the server ignores them if present.
type Envelope struct {
	V       int             `json:"v"`
	Seq     int64           `json:"seq"`
	TS      string          `json:"ts"`
	Type    string          `json:"type"`
	HandID  *int            `json:"hand_id"`
	Payload json.RawMessage `json:"payload"`
}

// NewEnvelope builds an Envelope around payload, marshaled to JSON. Seq and
// TS are left zero/empty: the hub fills them in when it actually sends the
// message. handID is nil for messages that don't belong to any hand.
func NewEnvelope(typ string, handID *int, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, fmt.Errorf("protocol: marshal %s payload: %w", typ, err)
	}
	return Envelope{V: ProtocolVersion, Type: typ, HandID: handID, Payload: raw}, nil
}

// Decode unmarshals e.Payload into v (a pointer to the concrete payload
// struct for e.Type).
func (e Envelope) Decode(v any) error {
	return json.Unmarshal(e.Payload, v)
}

// WithSeq returns a copy of e stamped with seq and the current timestamp.
func (e Envelope) WithSeq(seq int64) Envelope {
	e.Seq = seq
	e.TS = Now()
	return e
}

// IntPtr is a small helper so callers don't need &x literals for an
// *int field (HandID, Seat, LastSeq, ...).
func IntPtr(v int) *int { return &v }

// Int64Ptr mirrors IntPtr for *int64 fields (LastSeq).
func Int64Ptr(v int64) *int64 { return &v }

// StringPtr mirrors IntPtr for *string fields (PersonaID, Model).
func StringPtr(v string) *string { return &v }

// Float64Ptr mirrors IntPtr for *float64 fields (control.value).
func Float64Ptr(v float64) *float64 { return &v }
