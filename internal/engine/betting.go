package engine

import "fmt"

// legalActions computes what seat may legally do right now. It returns
// nil if it isn't seat's turn (or the hand is over).
func (h *Hand) legalActions(seat int) []LegalAction {
	if h.done || h.toAct != seat {
		return nil
	}
	p := h.players[seat]
	if p == nil || p.Status != Active {
		return nil
	}

	var acts []LegalAction
	acts = append(acts, LegalAction{Type: Fold})

	toCall := h.currentBet - p.BetThisStreet
	if toCall <= 0 {
		acts = append(acts, LegalAction{Type: Check})
	} else {
		acts = append(acts, LegalAction{Type: Call, Amount: min(toCall, p.Stack)})
	}

	if h.currentBet == 0 {
		if p.Stack > 0 {
			acts = append(acts, LegalAction{
				Type: Bet,
				Min:  min(h.cfg.BigBlind, p.Stack),
				Max:  p.Stack,
			})
		}
	} else {
		avail := p.Stack + p.BetThisStreet
		if p.CanRaise && avail > h.currentBet {
			minRaiseTo := h.currentBet + h.lastFullRaiseSize
			if minRaiseTo > avail {
				minRaiseTo = avail
			}
			acts = append(acts, LegalAction{Type: Raise, Min: minRaiseTo, Max: avail})
		}
	}

	// Shoving more than the call amount is a raise, which a seat that may
	// not re-raise (incomplete all-in didn't reopen betting) can't make.
	if p.Stack > 0 && (p.CanRaise || h.currentBet == 0 || p.Stack <= toCall) {
		acts = append(acts, LegalAction{Type: AllIn, Amount: p.Stack})
	}
	return acts
}

func findLegal(acts []LegalAction, t ActionType) (LegalAction, bool) {
	for _, a := range acts {
		if a.Type == t {
			return a, true
		}
	}
	return LegalAction{}, false
}

// apply validates `a` fully before mutating any state, then mutates and
// advances the hand (closing the betting round, dealing streets, running
// out the board, resolving showdown) as far as it automatically goes.
func (h *Hand) apply(seat int, a Action) ([]Event, error) {
	if h.done {
		return nil, fmt.Errorf("engine: hand is already over")
	}
	if h.toAct != seat {
		return nil, fmt.Errorf("engine: seat %d cannot act, expected %d", seat, h.toAct)
	}
	p := h.players[seat]
	legal := h.legalActions(seat)
	la, ok := findLegal(legal, a.Type)
	if !ok {
		return nil, fmt.Errorf("engine: action %q is not legal for seat %d", a.Type, seat)
	}

	// Resolve the concrete chip movement for this action without
	// mutating anything yet, so an invalid Amount leaves state intact.
	var added int  // chips moving from stack to pot this action
	var newBet int // player's BetThisStreet after this action
	switch a.Type {
	case Fold:
		// nothing to validate
	case Check:
		// nothing to validate
	case Call:
		added = la.Amount
		newBet = p.BetThisStreet + added
	case AllIn:
		added = p.Stack
		newBet = p.BetThisStreet + added
	case Bet:
		if a.Amount < la.Min || a.Amount > la.Max {
			return nil, fmt.Errorf("engine: bet amount %d out of range [%d,%d]", a.Amount, la.Min, la.Max)
		}
		newBet = a.Amount
		added = newBet - p.BetThisStreet
	case Raise:
		if a.Amount < la.Min || a.Amount > la.Max {
			return nil, fmt.Errorf("engine: raise amount %d out of range [%d,%d]", a.Amount, la.Min, la.Max)
		}
		newBet = a.Amount
		added = newBet - p.BetThisStreet
	default:
		return nil, fmt.Errorf("engine: unknown action type %q", a.Type)
	}

	events := h.commit(seat, a.Type, added, newBet)
	// Recompute who's next in rotation order after the seat that just
	// acted; closeOrAdvance only handles what happens once nobody (or
	// at most one player) still needs to act.
	h.toAct = h.scanForAction(h.seatAfter(seat))
	events = append(events, h.closeOrAdvance()...)
	return events, nil
}

// commit mutates state for a validated action and returns its action
// event. oldCurrentBet is captured before h.currentBet may change.
func (h *Hand) commit(seat int, t ActionType, added, newBet int) []Event {
	p := h.players[seat]
	oldCurrentBet := h.currentBet

	switch t {
	case Fold:
		p.Status = Folded
	case Check:
		p.ActedThisStreet = true
	case Call:
		p.Stack -= added
		p.BetThisStreet = newBet
		p.TotalInPot += added
		if p.Stack == 0 {
			p.Status = AllInState
		}
		p.ActedThisStreet = true
	case Bet, Raise, AllIn:
		p.Stack -= added
		p.BetThisStreet = newBet
		p.TotalInPot += added
		if p.Stack == 0 {
			p.Status = AllInState
		}
		p.ActedThisStreet = true

		if newBet > oldCurrentBet {
			increment := newBet - oldCurrentBet
			// h.lastFullRaiseSize is the floor (BB) at the start of every
			// street, so this one comparison covers both "first bet of
			// the street" (oldCurrentBet==0) and ordinary raises.
			full := increment >= h.lastFullRaiseSize
			h.currentBet = newBet
			if full {
				h.lastFullRaiseSize = increment
				for _, s := range h.seatOrder {
					if s == seat {
						continue
					}
					op := h.players[s]
					if op.Status == Active {
						op.ActedThisStreet = false
						op.CanRaise = true
					}
				}
			} else {
				for _, s := range h.seatOrder {
					if s == seat {
						continue
					}
					op := h.players[s]
					if op.Status == Active && op.ActedThisStreet {
						op.CanRaise = false
					}
				}
			}
		}
	}

	return []Event{{
		Kind:       EvAction,
		Seat:       seat,
		Action:     Action{Type: t, Amount: added},
		StackAfter: p.Stack,
		PotAfter:   h.totalPot(),
	}}
}

// closeOrAdvance resolves everything that happens automatically once no
// human/agent decision is pending: fold-to-one wins, closing a betting
// round, dealing the next street (or running out every remaining street
// when at most one player can still act), and showdown.
func (h *Hand) closeOrAdvance() []Event {
	var events []Event

	if nf := h.nonFoldedSeats(); len(nf) == 1 {
		return append(events, h.resolveFoldWin(nf[0])...)
	}

	for h.toAct == noSeat {
		if h.street == River {
			return append(events, h.resolveShowdown()...)
		}
		next := nextStreet(h.street)
		events = append(events, h.dealStreet(next))
		h.startStreetBetting()

		if len(h.activeSeats()) <= 1 {
			// Nobody left who can bet further; keep dealing streets
			// without reopening betting, straight through to showdown.
			h.toAct = noSeat
			continue
		}
	}
	return events
}

func nextStreet(s Street) Street {
	switch s {
	case Preflop:
		return Flop
	case Flop:
		return Turn
	case Turn:
		return River
	default:
		return Showdown
	}
}

// resolveFoldWin awards the whole pot to the lone remaining seat without
// a showdown/reveal.
func (h *Hand) resolveFoldWin(winner int) []Event {
	h.returnUncalledBet()
	amount := h.totalPot()

	p := h.players[winner]
	p.Stack += amount
	// TotalInPot/BetThisStreet are left as-is (not zeroed): they still
	// accurately describe what each seat put in this hand, and keeping
	// them makes "pots sum == contributions" checkable uniformly with
	// the showdown path.

	pot := Pot{Amount: amount, Eligible: []int{winner}, Winners: []int{winner}}
	h.pots = []Pot{pot}
	h.done = true
	h.toAct = noSeat

	events := []Event{
		{Kind: EvPotAwarded, Pots: []Pot{pot}},
		{Kind: EvHandEnd, Stacks: h.finalStacks()},
	}
	return events
}

// resolveShowdown reveals every non-folded seat, builds and awards every
// pot, and ends the hand.
func (h *Hand) resolveShowdown() []Event {
	h.returnUncalledBet()

	nonFolded := h.nonFoldedSeats()
	reveals := make([]Reveal, 0, len(nonFolded))
	for _, seat := range nonFolded {
		p := h.players[seat]
		code := evaluate7(p.Hole, h.board)
		class := defaultEvaluator.Class(code)
		h.revealed[seat] = copyCards(p.Hole)
		reveals = append(reveals, Reveal{
			Seat:     seat,
			Cards:    copyCards(p.Hole),
			Rank:     postflopDescription(class, append(copyCards(p.Hole), h.board...)),
			RankCode: int(code),
		})
	}

	pots := h.buildPots()
	h.awardPots(pots)
	h.pots = pots
	h.done = true
	h.toAct = noSeat
	h.street = Showdown

	events := []Event{
		{Kind: EvShowdown, Reveals: reveals},
		{Kind: EvPotAwarded, Pots: pots},
		{Kind: EvHandEnd, Stacks: h.finalStacks()},
	}
	return events
}

func (h *Hand) finalStacks() map[int]int {
	out := make(map[int]int, len(h.players))
	for seat, p := range h.players {
		out[seat] = p.Stack
	}
	return out
}
