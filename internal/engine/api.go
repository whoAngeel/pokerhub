// Package engine is the deterministic Texas Hold'em rules engine.
// It is pure: no I/O, no network, no LLM, no goroutines, no time.Now().
// Randomness only enters through an injected *rand.Rand.
//
// # Event sequence
//
// NewHand returns, in order:
//  1. hand_start  (Seat = button, Stacks = starting stack per seat)
//  2. blind        one event per posted blind, in order SB then BB
//     (Action.Type = Bet, Action.Amount = chips posted;
//     short-stack blinds post less and the seat goes all-in)
//  3. hole_cards   one event per dealt-in seat, in ascending seat order
//     (Cards = that seat's two hole cards)
//
// Apply(seat, action) returns, in order, a subset of:
//  1. action       always first (Action.Amount = chips ADDED by this
//     action, StackAfter, PotAfter)
//  2. street       zero or more, each with the full cumulative Board,
//     emitted when a betting round closes and the engine
//     deals the next street; when 2+ players can still act
//     only one street event is emitted (next street opens
//     for betting); when at most one player can still act
//     the engine auto-deals every remaining street in a row
//     (a "run out") before showdown
//  3. showdown     only when 2+ players reach showdown; Reveals cover
//     every non-folded seat (no reveal on fold-to-one wins)
//  4. pot_awarded  always present once the hand ends; Pots covers every
//     main/side pot with Winners filled in
//  5. hand_end     always last; Stacks = final stack per seat
//
// A hand that ends because everyone but one player folded skips street
// run-outs it doesn't need and skips showdown entirely, going straight
// from action to pot_awarded to hand_end.
//
// Apply validates an action fully before mutating anything: on error the
// Hand is left exactly as it was.
package engine

import "math/rand/v2"

// Card is a 2-char string: rank (23456789TJQKA) + suit (shdc), e.g. "Ah", "Td".
type Card string

type Street string

const (
	Preflop  Street = "preflop"
	Flop     Street = "flop"
	Turn     Street = "turn"
	River    Street = "river"
	Showdown Street = "showdown"
)

type ActionType string

const (
	Fold  ActionType = "fold"
	Check ActionType = "check"
	Call  ActionType = "call"
	Bet   ActionType = "bet"
	Raise ActionType = "raise"
	AllIn ActionType = "all_in"
)

// Action is what a player submits. For bet/raise, Amount is the TOTAL
// the player's bet for this street becomes ("raise to"), not the increment.
// For fold/check/call/all_in, Amount is ignored by Apply (the engine
// computes the actual chip movement deterministically).
type Action struct {
	Type   ActionType
	Amount int
}

// LegalAction describes one allowed action. Call/AllIn use Amount (chips
// added by this action); Bet/Raise use Min/Max ("raise to" totals).
type LegalAction struct {
	Type   ActionType
	Amount int
	Min    int
	Max    int
}

type SeatStatus string

const (
	Active     SeatStatus = "active"
	Folded     SeatStatus = "folded"
	AllInState SeatStatus = "all_in"
	SittingOut SeatStatus = "sitting_out"
)

// SeatState is the input to NewHand: who sits where and with how many chips.
// Seats with Stack == 0 or SittingOut are dealt out.
type SeatState struct {
	Seat   int
	Stack  int
	Status SeatStatus
}

type HandConfig struct {
	HandID     int
	SmallBlind int
	BigBlind   int
}

// PublicSeat is a seat as everybody sees it.
type PublicSeat struct {
	Seat          int
	Stack         int
	Status        SeatStatus
	BetThisStreet int
	TotalInPot    int
}

// PublicState never contains hole cards (except Revealed at showdown).
type PublicState struct {
	HandID   int
	Street   Street
	Button   int
	SB, BB   int
	Board    []Card
	Pot      int // total chips committed this hand, including current street bets
	Seats    []PublicSeat
	ToAct    int // -1 if nobody
	ToCall   int // for the seat in ToAct
	Revealed map[int][]Card
}

// PlayerView is PublicState plus ONLY the given seat's hole cards.
type PlayerView struct {
	Public PublicState
	Seat   int
	Hole   []Card
}

type EventKind string

const (
	EvHandStart  EventKind = "hand_start"
	EvBlind      EventKind = "blind"
	EvHoleCards  EventKind = "hole_cards"
	EvAction     EventKind = "action"
	EvStreet     EventKind = "street"
	EvShowdown   EventKind = "showdown"
	EvPotAwarded EventKind = "pot_awarded"
	EvHandEnd    EventKind = "hand_end"
)

// Pot is one main/side pot; Winners filled on award.
type Pot struct {
	Amount   int
	Eligible []int
	Winners  []int
}

type Reveal struct {
	Seat     int
	Cards    []Card
	Rank     string // human readable (Spanish), e.g. "pareja de reyes"
	RankCode int    // lower is better (evaluator-native)
}

// Event is a domain event. Only fields relevant to Kind are set.
//
// Button is set on EvHandStart (the button seat). Stacks is set on both
// EvHandStart (starting stacks) and EvHandEnd (final stacks).
type Event struct {
	Kind       EventKind
	Seat       int
	Action     Action // for EvAction (Amount = chips added by this action)
	StackAfter int
	PotAfter   int
	Street     Street
	Board      []Card
	Button     int         // EvHandStart
	Cards      []Card      // EvHoleCards
	Reveals    []Reveal    // EvShowdown
	Pots       []Pot       // EvPotAwarded
	Stacks     map[int]int // EvHandStart (starting), EvHandEnd (final)
}

// Hand is one hand of No-Limit Hold'em. See hand.go for the struct fields
// and betting.go/pots.go for the state machine.

// NewHand shuffles with rng, posts blinds, deals hole cards. Returns the
// hand and the initial events (hand_start, blinds, hole_cards).
//
// Preconditions (violating these is a programming error and panics, not
// a game-flow error): at least 2 seats must be dealt in (Stack>0 and not
// SittingOut), and button must name one of the seats passed in.
func NewHand(cfg HandConfig, seats []SeatState, button int, rng *rand.Rand) (*Hand, []Event) {
	return newHand(cfg, seats, button, rng)
}

// ToAct returns the seat that must act; ok=false when the hand is over.
func (h *Hand) ToAct() (seat int, ok bool) { return h.toAct, !h.done && h.toAct != noSeat }

func (h *Hand) LegalActions(seat int) []LegalAction { return h.legalActions(seat) }

// Apply validates and applies an action. On error the hand is unchanged.
// It auto-advances streets, runs out the board when no more betting is
// possible, and resolves showdown/awards, emitting all resulting events.
func (h *Hand) Apply(seat int, a Action) ([]Event, error) { return h.apply(seat, a) }

func (h *Hand) Done() bool                  { return h.done }
func (h *Hand) Public() PublicState         { return h.public() }
func (h *Hand) ViewFor(seat int) PlayerView { return h.viewFor(seat) }

// Helpers for agents (computed by the engine, not by the LLM).

// MadeHand describes the best hand of hole+board in Spanish, e.g.
// "pareja de reyes". Preflop: "AK de distinto palo", "pareja de ochos".
func MadeHand(hole, board []Card) string { return madeHand(hole, board) }

// Equity estimates win probability (ties count as share) vs n random
// opponent hands with Monte Carlo over iters samples.
func Equity(hole, board []Card, opponents, iters int, rng *rand.Rand) float64 {
	return equity(hole, board, opponents, iters, rng)
}
