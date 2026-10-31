package protocol

import (
	"sort"
	"sync"
)

// seatState is TableState's mutable per-seat record. Name/PersonaID/Kind/
// Model never change after NewTableState (they come from table composition,
// not the event stream); Stack/Status/BetThisStreet/Hole are updated by
// Apply as events arrive.
type seatState struct {
	name          string
	personaID     *string
	kind          string
	model         *string
	stack         int
	status        string
	betThisStreet int
	hole          []Card
}

// TableState is a god-view reducer over the Envelope stream Convert
// produces: Apply(env) folds one envelope into the running state, and
// Snapshot() renders the current state as a fresh "snapshot" Envelope
// (always god-view -- every known hole card, legal_actions whenever
// somebody is to act). server.Project is what turns that god-view snapshot
// into whatever a given role/seat may actually see.
//
// This is also what the replay side uses: a replay.Player can rebuild
// TableState purely from the recorded .jsonl stream, with no dependency on
// internal/table/player/engine at replay time.
//
// Not safe for concurrent use without the caller's own locking EXCEPT that
// Apply/Snapshot/Seats each take the internal lock themselves, so the
// zero-contention path (one writer via Hub.Emit, readers via Hub's
// connection goroutines) is safe.
type TableState struct {
	mu sync.Mutex

	handID     int // 0 = no hand yet
	street     string
	button     int
	smallBlind int
	bigBlind   int
	board      []Card
	pot        int
	toAct      int // -1 = nobody
	toCall     int
	legal      []LegalAction

	seats []int // seat numbers, stable ascending order
	state map[int]*seatState
}

// NewTableState seeds a TableState from the table's static composition
// (PROTOCOL.md §3's Seat, minus the per-hand fields): every seat starts
// "active" at its configured buy-in, no bet, no known hole cards, and
// to_act -1 (nobody is mid-decision before the first hand starts).
func NewTableState(seats []Seat) *TableState {
	ts := &TableState{
		toAct: -1,
		state: make(map[int]*seatState, len(seats)),
	}
	for _, s := range seats {
		ts.seats = append(ts.seats, s.Seat)
		ts.state[s.Seat] = &seatState{
			name:      s.Name,
			personaID: s.PersonaID,
			kind:      s.Kind,
			model:     s.Model,
			stack:     s.Stack,
			status:    s.Status,
		}
	}
	sort.Ints(ts.seats)
	return ts
}

// Apply folds one god-view Envelope into the running state. Unknown types
// are ignored (PROTOCOL.md §8: a client, and by extension this reducer,
// must be able to ignore a type it doesn't recognize without breaking).
//
// TypeSnapshot is handled too, even though Hub never routes one through
// Apply itself (it builds/sends a snapshot straight from TableState.Snapshot
// on connect, bypassing Apply entirely -- see server/hub.go's publish vs.
// Connect/Replay). A standalone consumer that only sees the wire stream
// (e.g. internal/tui, or any other external client) has no such shortcut:
// the first Envelope it ever receives per PROTOCOL.md §2 IS a snapshot, so
// Apply needs to be able to seed/resync a fresh TableState directly from
// one, including after a reconnect mid-hand.
func (ts *TableState) Apply(env Envelope) error {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	switch env.Type {
	case TypeSnapshot:
		var p SnapshotPayload
		if err := env.Decode(&p); err != nil {
			return err
		}
		if p.HandID != nil {
			ts.handID = *p.HandID
		} else {
			ts.handID = 0
		}
		ts.street = p.Street
		ts.button = p.Button
		ts.smallBlind = p.SmallBlind
		ts.bigBlind = p.BigBlind
		ts.board = p.Board
		ts.pot = p.Pot
		ts.toAct = p.ToAct
		ts.toCall = 0 // not carried by SnapshotPayload; the next "turn" event fills it in
		ts.legal = p.LegalActions
		ts.seats = make([]int, len(p.Seats))
		ts.state = make(map[int]*seatState, len(p.Seats))
		for i, s := range p.Seats {
			ts.seats[i] = s.Seat
			ts.state[s.Seat] = &seatState{
				name: s.Name, personaID: s.PersonaID, kind: s.Kind, model: s.Model,
				stack: s.Stack, status: s.Status, betThisStreet: s.BetThisStreet, hole: s.Hole,
			}
		}
		sort.Ints(ts.seats)

	case TypeHandStart:
		var p HandStartPayload
		if err := env.Decode(&p); err != nil {
			return err
		}
		ts.handID = p.HandID
		ts.button = p.Button
		ts.smallBlind = p.SmallBlind
		ts.bigBlind = p.BigBlind
		ts.street = "preflop" // implicit: engine never emits a preflop street event
		ts.board = nil
		ts.pot = 0
		ts.toAct = -1
		ts.legal = nil
		for _, s := range p.Seats {
			st := ts.seatFor(s.Seat)
			st.stack = s.Stack
			st.status = "active"
			st.betThisStreet = 0
			st.hole = nil
		}

	case TypeBlind:
		var p BlindPayload
		if err := env.Decode(&p); err != nil {
			return err
		}
		st := ts.seatFor(p.Seat)
		st.stack = p.StackAfter
		st.betThisStreet += p.Amount
		ts.pot = p.PotAfter

	case TypeHoleCards:
		var p HoleCardsPayload
		if err := env.Decode(&p); err != nil {
			return err
		}
		ts.seatFor(p.Seat).hole = p.Cards

	case TypeStreet:
		var p StreetPayload
		if err := env.Decode(&p); err != nil {
			return err
		}
		ts.street = p.Street
		ts.board = p.Board
		for _, seat := range ts.seats {
			ts.state[seat].betThisStreet = 0
		}

	case TypeTurn:
		var p TurnPayload
		if err := env.Decode(&p); err != nil {
			return err
		}
		ts.toAct = p.Seat
		ts.toCall = p.ToCall
		ts.legal = p.LegalActions

	case TypeAction:
		var p ActionPayload
		if err := env.Decode(&p); err != nil {
			return err
		}
		st := ts.seatFor(p.Seat)
		st.stack = p.StackAfter
		st.betThisStreet += p.Amount
		switch p.Type {
		case "fold":
			st.status = "folded"
		case "all_in":
			st.status = "all_in"
		default:
			st.status = "active"
		}
		ts.pot = p.PotAfter
		ts.toAct = -1
		ts.legal = nil

	case TypeShowdown, TypeChat, TypeThinking, TypeReasoning, TypeAgentError, TypeMemoryUpdate:
		// No snapshot-relevant persistent state: showdown reveals ride on
		// their own public event, chat/thinking/reasoning/agent_error/
		// memory_update aren't part of the table's physical state.

	case TypePotAwarded:
		var p PotAwardedPayload
		if err := env.Decode(&p); err != nil {
			return err
		}
		_ = p // nothing to retain: hand_end carries the resulting stacks

	case TypeHandEnd:
		var p HandEndPayload
		if err := env.Decode(&p); err != nil {
			return err
		}
		ts.toAct = -1
		ts.legal = nil
		for _, s := range p.Seats {
			st := ts.seatFor(s.Seat)
			st.stack = s.Stack
			st.betThisStreet = 0
		}

	case TypeRebuy:
		var p RebuyPayload
		if err := env.Decode(&p); err != nil {
			return err
		}
		st := ts.seatFor(p.Seat)
		st.stack = p.StackAfter
		st.status = "active"
	}

	return nil
}

// seatFor returns seat's mutable record, creating a bare one if Apply sees
// a seat NewTableState wasn't told about (defensive; shouldn't happen when
// TableState is seeded from the same config that built the players).
func (ts *TableState) seatFor(seat int) *seatState {
	st, ok := ts.state[seat]
	if !ok {
		st = &seatState{status: "active"}
		ts.state[seat] = st
		ts.seats = append(ts.seats, seat)
		sort.Ints(ts.seats)
	}
	return st
}

// Snapshot renders the current state as a fresh god-view "snapshot"
// Envelope (Seq/TS zero; the hub stamps them like any other outgoing
// envelope). HandID is nil before the first hand starts.
func (ts *TableState) Snapshot() (Envelope, error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()

	var handID *int
	if ts.handID != 0 {
		handID = IntPtr(ts.handID)
	}

	seats := make([]SnapshotSeat, len(ts.seats))
	for i, seat := range ts.seats {
		st := ts.state[seat]
		seats[i] = SnapshotSeat{
			Seat: seat, Name: st.name, PersonaID: st.personaID, Kind: st.kind,
			Stack: st.stack, Status: st.status, BetThisStreet: st.betThisStreet, Model: st.model,
			Hole: st.hole,
		}
	}

	var legal []LegalAction
	if ts.toAct >= 0 {
		legal = ts.legal
	}

	payload := SnapshotPayload{
		HandID: handID, Street: ts.street, Button: ts.button,
		SmallBlind: ts.smallBlind, BigBlind: ts.bigBlind, Board: ts.board,
		Pot: ts.pot, ToAct: ts.toAct, Seats: seats, LegalActions: legal,
	}
	return NewEnvelope(TypeSnapshot, handID, payload)
}
