// Package memory computes deterministic per-seat stats from the engine's
// public event stream (PLAN §7). It never sees hole cards except the ones
// engine.Event already exposes publicly at showdown (Reveal), so every
// observer computes the exact same numbers: there is no way to leak
// information through memory.
package memory

import (
	"github.com/whoAngeel/pokerhub/internal/engine"
	"github.com/whoAngeel/pokerhub/internal/player"
)

// handState is the per-hand scratch space Tracker needs while a hand is in
// progress; it is reset on every EvHandStart.
type handState struct {
	street engine.Street

	// vpipCounted/pfrCounted guard against double counting: a seat can act
	// several times preflop (e.g. call a raise then later be re-raised),
	// but VPIP/PFR count the HAND, not the action.
	vpipCounted map[int]bool
	pfrCounted  map[int]bool

	riverAggressor  map[int]bool // seat bet or raised at least once on the river
	reachedShowdown map[int]bool
	winners         map[int]bool
}

func newHandState() *handState {
	return &handState{
		vpipCounted:     map[int]bool{},
		pfrCounted:      map[int]bool{},
		riverAggressor:  map[int]bool{},
		reachedShowdown: map[int]bool{},
		winners:         map[int]bool{},
	}
}

// seatTotals accumulates across the whole session (every hand), the
// denominators and numerators behind player.Stats.
type seatTotals struct {
	hands            int
	vpip             int
	pfr              int
	betsAndRaises    int // AF numerator, every street, every hand
	calls            int // AF denominator
	riverBluffsShown int
}

// Tracker computes player.Stats per seat from a stream of engine.Event,
// fed one event at a time in the exact order Apply/NewHand emit them. It is
// not safe for concurrent use (the table drives it from a single goroutine,
// same as it drives the Hand itself).
type Tracker struct {
	totals map[int]*seatTotals
	hand   *handState
}

// NewTracker returns an empty Tracker.
func NewTracker() *Tracker {
	return &Tracker{totals: map[int]*seatTotals{}}
}

func (t *Tracker) totalsFor(seat int) *seatTotals {
	s, ok := t.totals[seat]
	if !ok {
		s = &seatTotals{}
		t.totals[seat] = s
	}
	return s
}

// Record processes one engine.Event, updating accumulated totals. Call it
// for every event NewHand/Apply return, in order.
func (t *Tracker) Record(ev engine.Event) {
	switch ev.Kind {
	case engine.EvHandStart:
		t.hand = newHandState()
		t.hand.street = engine.Preflop

	case engine.EvHoleCards:
		t.totalsFor(ev.Seat).hands++

	case engine.EvStreet:
		if t.hand != nil {
			t.hand.street = ev.Street
		}

	case engine.EvAction:
		t.recordAction(ev)

	case engine.EvShowdown:
		if t.hand == nil {
			return
		}
		for _, r := range ev.Reveals {
			t.hand.reachedShowdown[r.Seat] = true
		}

	case engine.EvPotAwarded:
		if t.hand == nil {
			return
		}
		for _, pot := range ev.Pots {
			for _, w := range pot.Winners {
				t.hand.winners[w] = true
			}
		}

	case engine.EvHandEnd:
		t.finalizeHand()
	}
}

func (t *Tracker) recordAction(ev engine.Event) {
	if t.hand == nil {
		return
	}
	totals := t.totalsFor(ev.Seat)

	switch ev.Action.Type {
	case engine.Bet, engine.Raise, engine.AllIn:
		totals.betsAndRaises++
		if t.hand.street == engine.Preflop && !t.hand.pfrCounted[ev.Seat] {
			t.hand.pfrCounted[ev.Seat] = true
			totals.pfr++
		}
		if t.hand.street == engine.Preflop && !t.hand.vpipCounted[ev.Seat] {
			t.hand.vpipCounted[ev.Seat] = true
			totals.vpip++
		}
		if t.hand.street == engine.River {
			t.hand.riverAggressor[ev.Seat] = true
		}

	case engine.Call:
		totals.calls++
		if t.hand.street == engine.Preflop && !t.hand.vpipCounted[ev.Seat] {
			t.hand.vpipCounted[ev.Seat] = true
			totals.vpip++
		}

	case engine.Check, engine.Fold:
		// Neither VPIP, PFR nor AF numerator/denominator: checking costs no
		// chips and folding never called.
	}
}

// finalizeHand applies the river-bluff rule once the hand is fully resolved
// (PLAN §7, kept simple per the task spec): a seat that bet/raised the river
// AND reached showdown AND did not win any pot counts as one shown bluff.
func (t *Tracker) finalizeHand() {
	if t.hand == nil {
		return
	}
	for seat := range t.hand.riverAggressor {
		if t.hand.reachedShowdown[seat] && !t.hand.winners[seat] {
			t.totalsFor(seat).riverBluffsShown++
		}
	}
	t.hand = nil
}

// Stats returns the current player.Stats snapshot for seat (zero value if
// the seat has never been dealt in).
func (t *Tracker) Stats(seat int) player.Stats {
	totals, ok := t.totals[seat]
	if !ok {
		return player.Stats{}
	}
	return player.Stats{
		Hands:            totals.hands,
		VPIP:             ratio(totals.vpip, totals.hands),
		PFR:              ratio(totals.pfr, totals.hands),
		AF:               ratio(totals.betsAndRaises, totals.calls),
		RiverBluffsShown: totals.riverBluffsShown,
	}
}

// StatsMap returns a snapshot for every seat Record has ever seen.
func (t *Tracker) StatsMap() map[int]player.Stats {
	out := make(map[int]player.Stats, len(t.totals))
	for seat := range t.totals {
		out[seat] = t.Stats(seat)
	}
	return out
}

// ratio is num/den, 0 when den is 0 (no sample yet, rather than NaN/Inf).
func ratio(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}
