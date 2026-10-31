package server

import "github.com/whoAngeel/pokerhub/internal/protocol"

// Project is the SINGLE visibility filter (docs/PROTOCOL.md §1): every
// Envelope the hub is about to deliver to a connection goes through Project
// first, and the hub trusts its answer completely -- nothing downstream
// re-checks visibility.
//
//   - Spectator: sees everything, unmodified.
//   - player:<seat>: sees every public event unmodified; hole_cards only
//     for its own seat; turn.legal_actions only for its own seat (the event
//     itself still delivers, legal_actions just comes back nil); never
//     reasoning, memory_update or agent_error; snapshot is filtered the
//     same way field-by-field (hole cards, legal_actions).
//
// Every table.EventType has an explicit case below. A type Project doesn't
// recognize (a future addition this build predates) fails CLOSED for a
// player (not delivered) -- spectator still gets it, since "spectator sees
// everything" has no exceptions. This is deliberate: an unrecognized type
// might carry something sensitive (another reasoning-shaped event, say),
// and PROTOCOL.md §8 only promises additive changes are safe for a client
// to *ignore*, not that every observer safely gets to see them.
func Project(env protocol.Envelope, role Role) (protocol.Envelope, bool) {
	if role.IsSpectator() {
		return env, true
	}

	switch env.Type {
	// Private to spectator only, no exceptions -- not even the seat the
	// event is about (PROTOCOL.md §4 "reasoning": "deliberadamente privado
	// incluso para su dueño").
	case protocol.TypeReasoning, protocol.TypeMemoryUpdate, protocol.TypeAgentError:
		return protocol.Envelope{}, false

	case protocol.TypeHoleCards:
		var p protocol.HoleCardsPayload
		if err := env.Decode(&p); err != nil {
			return protocol.Envelope{}, false
		}
		if !role.Sees(p.Seat) {
			return protocol.Envelope{}, false
		}
		return env, true

	case protocol.TypeTurn:
		var p protocol.TurnPayload
		if err := env.Decode(&p); err != nil {
			return protocol.Envelope{}, false
		}
		if role.Sees(p.Seat) {
			return env, true
		}
		p.LegalActions = nil
		return reEncode(env, p)

	case protocol.TypeSnapshot:
		return projectSnapshot(env, role)

	// Public, delivered unmodified to every role.
	case protocol.TypeHandStart, protocol.TypeBlind, protocol.TypeStreet, protocol.TypeThinking,
		protocol.TypeAction, protocol.TypeChat, protocol.TypeShowdown, protocol.TypePotAwarded,
		protocol.TypeHandEnd, protocol.TypeRebuy:
		return env, true

	default:
		// Fail closed: an unlisted type never reaches a player.
		return protocol.Envelope{}, false
	}
}

// projectSnapshot filters a god-view SnapshotPayload the same way the rest
// of Project filters everything else: hole cards and legal_actions, field
// by field, never a wholesale re-fetch.
func projectSnapshot(env protocol.Envelope, role Role) (protocol.Envelope, bool) {
	var p protocol.SnapshotPayload
	if err := env.Decode(&p); err != nil {
		return protocol.Envelope{}, false
	}

	seats := make([]protocol.SnapshotSeat, len(p.Seats))
	for i, s := range p.Seats {
		if !role.Sees(s.Seat) {
			s.Hole = nil
		}
		seats[i] = s
	}
	p.Seats = seats

	if !role.Sees(p.ToAct) {
		p.LegalActions = nil
	}

	return reEncode(env, p)
}

// reEncode rebuilds env around a (filtered) payload, keeping V/Seq/TS/Type/
// HandID untouched.
func reEncode(env protocol.Envelope, payload any) (protocol.Envelope, bool) {
	out, err := protocol.NewEnvelope(env.Type, env.HandID, payload)
	if err != nil {
		return protocol.Envelope{}, false
	}
	out.V, out.Seq, out.TS = env.V, env.Seq, env.TS
	return out, true
}
