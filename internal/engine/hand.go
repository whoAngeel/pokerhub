package engine

import (
	"math/rand/v2"
	"sort"
)

// noSeat marks "nobody" for toAct.
const noSeat = -1

// playerState is the internal, mutable record for one seat during a hand.
type playerState struct {
	Seat   int
	Stack  int
	Status SeatStatus
	Hole   []Card

	BetThisStreet int // chips committed this street
	TotalInPot    int // chips committed this whole hand

	ActedThisStreet bool // acted (check/call/bet/raise/all_in) since last reset
	CanRaise        bool // false while locked out by an incomplete all-in raise

	DealtIn bool // Stack>0 && Status != SittingOut at hand start
}

// Hand is one hand of No-Limit Hold'em. It holds the full, private state
// (including every seat's hole cards); Public()/ViewFor() are the only
// safe windows into it from the outside.
type Hand struct {
	cfg    HandConfig
	button int

	players   map[int]*playerState
	seatOrder []int // sorted dealt-in seat ids; fixed rotation for the hand

	street Street
	board  []Card
	deck   []Card
	deckAt int // next undealt index into deck

	currentBet        int // amount to match on the current street
	lastFullRaiseSize int // size of the last full bet/raise (floor = BB)

	toAct int // noSeat if nobody needs to act right now

	done bool

	pots     []Pot
	revealed map[int][]Card // filled at showdown only

	rng *rand.Rand
}

func newHand(cfg HandConfig, seats []SeatState, button int, rng *rand.Rand) (*Hand, []Event) {
	h := &Hand{
		cfg:      cfg,
		button:   button,
		players:  make(map[int]*playerState, len(seats)),
		street:   Preflop,
		board:    nil,
		revealed: make(map[int][]Card),
		rng:      rng,
	}

	startStacks := make(map[int]int, len(seats))
	for _, s := range seats {
		dealtIn := s.Stack > 0 && s.Status != SittingOut
		status := s.Status
		if status == "" {
			status = Active
		}
		if !dealtIn && status == Active {
			// Stack==0 but marked active: treat as sitting out for this hand.
			status = SittingOut
		}
		h.players[s.Seat] = &playerState{
			Seat:     s.Seat,
			Stack:    s.Stack,
			Status:   status,
			DealtIn:  dealtIn,
			CanRaise: true,
		}
		startStacks[s.Seat] = s.Stack
		if dealtIn {
			h.seatOrder = append(h.seatOrder, s.Seat)
		}
	}
	sort.Ints(h.seatOrder)

	if len(h.seatOrder) < 2 {
		panic("engine: NewHand requires at least 2 dealt-in seats")
	}
	if _, ok := h.players[button]; !ok {
		panic("engine: NewHand button must name a seat passed in seats")
	}

	h.deck = newDeck()
	shuffleDeck(h.deck, rng)

	events := []Event{{
		Kind:   EvHandStart,
		Button: button,
		Stacks: startStacks,
	}}

	events = append(events, h.postBlinds()...)

	// Deal hole cards, two at a time around the table starting from the
	// seat after the button, then emit hole_cards in ascending seat order
	// for a deterministic event stream.
	for pass := 0; pass < 2; pass++ {
		for _, seat := range h.seatOrder {
			h.players[seat].Hole = append(h.players[seat].Hole, h.draw())
		}
	}
	for _, seat := range h.seatOrder {
		events = append(events, Event{
			Kind:  EvHoleCards,
			Seat:  seat,
			Cards: copyCards(h.players[seat].Hole),
		})
	}

	h.startStreetBetting()
	events = append(events, h.closeOrAdvance()...)
	return h, events
}

func (h *Hand) draw() Card {
	c := h.deck[h.deckAt]
	h.deckAt++
	return c
}

// seatIndex returns the index of seat within seatOrder, or -1.
func (h *Hand) seatIndex(seat int) int {
	for i, s := range h.seatOrder {
		if s == seat {
			return i
		}
	}
	return -1
}

// seatAfter returns the next seat in rotation order after seat (cyclic
// over dealt-in seats; seat itself need not still be active).
func (h *Hand) seatAfter(seat int) int {
	n := len(h.seatOrder)
	idx := h.seatIndex(seat)
	if idx < 0 {
		idx = 0
	}
	return h.seatOrder[(idx+1)%n]
}

func (h *Hand) postBlinds() []Event {
	n := len(h.seatOrder)
	var sb, bb int
	if n == 2 {
		sb = h.button
		bb = h.seatAfter(h.button)
	} else {
		sb = h.seatAfter(h.button)
		bb = h.seatAfter(sb)
	}

	var events []Event
	events = append(events, h.postBlind(sb, h.cfg.SmallBlind))
	events = append(events, h.postBlind(bb, h.cfg.BigBlind))
	return events
}

func (h *Hand) postBlind(seat int, amount int) Event {
	p := h.players[seat]
	posted := min(amount, p.Stack)
	p.Stack -= posted
	p.BetThisStreet += posted
	p.TotalInPot += posted
	if p.Stack == 0 {
		p.Status = AllInState
	}
	if posted > h.currentBet {
		h.currentBet = posted
	}
	return Event{
		Kind:       EvBlind,
		Seat:       seat,
		Action:     Action{Type: Bet, Amount: posted},
		StackAfter: p.Stack,
		PotAfter:   h.totalPot(),
	}
}

// startStreetBetting resets per-street betting state (for h.street, which
// must already be set) and computes the first seat to act, honoring the
// heads-up vs 3+-way first-actor rules. It does not resolve a round that
// turns out to already be closed (e.g. everyone all-in) — see
// closeOrAdvance, which callers always invoke right after this.
func (h *Hand) startStreetBetting() {
	if h.street != Preflop {
		h.currentBet = 0
		for _, seat := range h.seatOrder {
			h.players[seat].BetThisStreet = 0
		}
	}
	h.lastFullRaiseSize = h.cfg.BigBlind
	for _, seat := range h.seatOrder {
		p := h.players[seat]
		p.ActedThisStreet = false
		p.CanRaise = true
	}

	n := len(h.seatOrder)
	var first int
	if h.street == Preflop {
		if n == 2 {
			first = h.button // heads-up: button/SB acts first preflop
		} else {
			sb := h.seatAfter(h.button)
			bb := h.seatAfter(sb)
			first = h.seatAfter(bb)
		}
	} else {
		// Heads-up and 3+-way both reduce to "first active seat after
		// the button" postflop (in heads-up that's the BB).
		first = h.seatAfter(h.button)
	}

	h.toAct = h.scanForAction(first)
}

// dealStreet advances h.street, deals the right number of new board
// cards, and returns the resulting street event (with the full
// cumulative board).
func (h *Hand) dealStreet(street Street) Event {
	n := 0
	switch street {
	case Flop:
		n = 3
	case Turn, River:
		n = 1
	}
	for i := 0; i < n; i++ {
		h.board = append(h.board, h.draw())
	}
	h.street = street
	return Event{Kind: EvStreet, Street: street, Board: copyCards(h.board)}
}

// needsAction reports whether p still must act to close the betting round.
func (h *Hand) needsAction(p *playerState) bool {
	return !(p.ActedThisStreet && p.BetThisStreet == h.currentBet)
}

// scanForAction returns the first seat, starting at (and including)
// start and moving forward in rotation order, that is Active and still
// needs to act; noSeat if none do.
func (h *Hand) scanForAction(start int) int {
	n := len(h.seatOrder)
	startIdx := h.seatIndex(start)
	if startIdx < 0 {
		return noSeat
	}
	for k := 0; k < n; k++ {
		seat := h.seatOrder[(startIdx+k)%n]
		p := h.players[seat]
		if p.Status == Active && h.needsAction(p) {
			return seat
		}
	}
	return noSeat
}

// nonFoldedSeats returns dealt-in seats that haven't folded, in
// rotation order.
func (h *Hand) nonFoldedSeats() []int {
	var out []int
	for _, seat := range h.seatOrder {
		if h.players[seat].Status != Folded {
			out = append(out, seat)
		}
	}
	return out
}

// activeSeats returns dealt-in seats that can still voluntarily act
// (not folded, not all-in).
func (h *Hand) activeSeats() []int {
	var out []int
	for _, seat := range h.seatOrder {
		if h.players[seat].Status == Active {
			out = append(out, seat)
		}
	}
	return out
}

func (h *Hand) totalPot() int {
	total := 0
	for _, seat := range h.seatOrder {
		total += h.players[seat].TotalInPot
	}
	return total
}

func (h *Hand) public() PublicState {
	seats := make([]PublicSeat, 0, len(h.players))
	allSeats := make([]int, 0, len(h.players))
	for seat := range h.players {
		allSeats = append(allSeats, seat)
	}
	sort.Ints(allSeats)
	for _, seat := range allSeats {
		p := h.players[seat]
		seats = append(seats, PublicSeat{
			Seat:          seat,
			Stack:         p.Stack,
			Status:        p.Status,
			BetThisStreet: p.BetThisStreet,
			TotalInPot:    p.TotalInPot,
		})
	}

	revealed := make(map[int][]Card, len(h.revealed))
	for seat, cards := range h.revealed {
		revealed[seat] = copyCards(cards)
	}

	toAct := noSeat
	toCall := 0
	if !h.done && h.toAct != noSeat {
		toAct = h.toAct
		toCall = h.currentBet - h.players[h.toAct].BetThisStreet
	}

	return PublicState{
		HandID:   h.cfg.HandID,
		Street:   h.street,
		Button:   h.button,
		SB:       h.cfg.SmallBlind,
		BB:       h.cfg.BigBlind,
		Board:    copyCards(h.board),
		Pot:      h.totalPot(),
		Seats:    seats,
		ToAct:    toAct,
		ToCall:   toCall,
		Revealed: revealed,
	}
}

func (h *Hand) viewFor(seat int) PlayerView {
	v := PlayerView{Public: h.public(), Seat: seat}
	if p, ok := h.players[seat]; ok {
		v.Hole = copyCards(p.Hole)
	}
	return v
}
