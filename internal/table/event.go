// Package table is the session orchestrator: it drives engine.Hand turn by
// turn, builds each seat's player.DecisionView, calls player.Decide, applies
// the result back into the engine, and emits a stream of table.Event for
// whoever is watching (a terminal printer in F2; a WS projector in F3).
//
// table may import internal/engine (it IS the trusted orchestrator); it must
// never hand an engine type or an opponent's hole cards to a player.Player
// -- only player.DecisionView, built from engine.(*Hand).ViewFor(seat).
package table

import (
	"github.com/whoAngeel/pokerhub/internal/engine"
	"github.com/whoAngeel/pokerhub/internal/player"
)

// EventType names one kind of table.Event, mapped close to PLAN §5's WS
// event table so a later protocol/server layer can convert 1:1.
type EventType string

const (
	EvHandStart  EventType = "hand_start"
	EvHoleCards  EventType = "hole_cards"
	EvBlind      EventType = "blind"
	EvStreet     EventType = "street"
	EvTurn       EventType = "turn"
	EvThinking   EventType = "thinking"
	EvAction     EventType = "action"
	EvChat       EventType = "chat"
	EvReasoning  EventType = "reasoning" // private: only ever meant for spectator/debug consumers
	EvShowdown   EventType = "showdown"
	EvPotAwarded EventType = "pot_awarded"
	EvHandEnd    EventType = "hand_end"
	EvRebuy      EventType = "rebuy"
	EvAgentError EventType = "agent_error" // private: fallback/warnings, never a player's own feed
)

// Event is one table-level occurrence. Only fields relevant to Type are
// populated; everything else is the zero value. Seat is -1 when the event
// isn't about one particular seat.
type Event struct {
	Type     EventType
	HandID   int
	Seat     int
	SeatName string

	// hand_start
	Button     int
	SmallBlind int
	BigBlind   int
	Stacks     map[int]int    // hand_start: starting stacks; hand_end: final stacks
	SeatNames  map[int]string // hand_start: seat -> display name, so a late subscriber can label seats

	// hole_cards
	Cards []engine.Card

	// blind / rebuy
	Amount int // blind: chips posted this blind; rebuy: chips added to reach buy_in

	// street
	Street engine.Street
	Board  []engine.Card

	// turn
	ToCall       int
	LegalActions []player.LegalAction

	// action
	Action     engine.Action // Type + Amount (chips ADDED by this action, per engine.Event's convention)
	StackAfter int
	PotAfter   int

	// chat
	Say string

	// reasoning
	Reasoning *player.Reasoning
	Meta      player.DecisionMeta

	// showdown
	Reveals []engine.Reveal

	// pot_awarded
	Pots []engine.Pot

	// agent_error
	Cause           string
	AttemptedAction engine.Action
}

// Sink receives every table.Event as it happens, in order.
type Sink interface {
	Emit(Event)
}

// SinkFunc adapts a plain function to Sink.
type SinkFunc func(Event)

func (f SinkFunc) Emit(e Event) { f(e) }

// MultiSink fans out to several sinks in order (e.g. a recorder + a printer).
type MultiSink []Sink

func (m MultiSink) Emit(e Event) {
	for _, s := range m {
		s.Emit(e)
	}
}
