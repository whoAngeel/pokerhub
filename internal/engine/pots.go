package engine

import "sort"

// returnUncalledBet finds the single seat (if any) whose total
// contribution this hand strictly exceeds every other seat's, and
// refunds the part nobody could have matched. Must run once, right
// before pots are built, whether the hand ends by fold or by showdown.
func (h *Hand) returnUncalledBet() {
	maxVal, secondVal := -1, -1
	maxSeat := -1
	tiesAtMax := 0

	for _, seat := range h.seatOrder {
		v := h.players[seat].TotalInPot
		switch {
		case v > maxVal:
			secondVal = maxVal
			maxVal = v
			maxSeat = seat
			tiesAtMax = 1
		case v == maxVal:
			tiesAtMax++
		case v > secondVal:
			secondVal = v
		}
	}

	if tiesAtMax != 1 || maxSeat < 0 {
		return
	}
	floor := secondVal
	if floor < 0 {
		floor = 0
	}
	if maxVal <= floor {
		return
	}

	refund := maxVal - floor
	p := h.players[maxSeat]
	p.TotalInPot -= refund
	p.Stack += refund
	p.BetThisStreet -= refund
	if p.BetThisStreet < 0 {
		p.BetThisStreet = 0
	}
}

// buildPots layers per-player total contributions into main/side pots.
// Eligibility for a layer is "non-folded and contributed at least this
// layer's level"; a layer that (defensively) ends up with no eligible
// seat is merged into a neighboring one so no chips are lost.
func (h *Hand) buildPots() []Pot {
	levelSet := map[int]bool{}
	for _, seat := range h.seatOrder {
		if v := h.players[seat].TotalInPot; v > 0 {
			levelSet[v] = true
		}
	}
	levels := make([]int, 0, len(levelSet))
	for v := range levelSet {
		levels = append(levels, v)
	}
	sort.Ints(levels)

	var pots []Pot
	prev := 0
	for _, level := range levels {
		slice := level - prev
		var eligible []int
		count := 0
		for _, seat := range h.seatOrder {
			p := h.players[seat]
			if p.TotalInPot >= level {
				count++
				if p.Status != Folded {
					eligible = append(eligible, seat)
				}
			}
		}
		amount := slice * count
		prev = level
		if amount <= 0 {
			continue
		}
		if len(eligible) == 0 {
			// No non-folded contributor reached this layer (shouldn't
			// happen given valid betting, but don't lose chips).
			if len(pots) > 0 {
				pots[len(pots)-1].Amount += amount
			} else {
				pots = append(pots, Pot{Amount: amount})
			}
			continue
		}
		pots = append(pots, Pot{Amount: amount, Eligible: eligible})
	}
	return pots
}

// awardPots evaluates each pot's eligible hands and fills in Winners,
// moving chips into winners' stacks. Odd chips go one by one to winners
// in seat order starting left of the button.
func (h *Hand) awardPots(pots []Pot) {
	for i := range pots {
		pot := &pots[i]
		if len(pot.Eligible) == 0 {
			continue
		}
		best := int32(0)
		codes := make(map[int]int32, len(pot.Eligible))
		for _, seat := range pot.Eligible {
			p := h.players[seat]
			code := evaluate7(p.Hole, h.board)
			codes[seat] = code
			if best == 0 || code < best {
				best = code
			}
		}
		var winners []int
		for _, seat := range pot.Eligible {
			if codes[seat] == best {
				winners = append(winners, seat)
			}
		}
		winners = h.orderFromLeftOfButton(winners)
		pot.Winners = winners

		share := pot.Amount / len(winners)
		remainder := pot.Amount % len(winners)
		for i, seat := range winners {
			amt := share
			if i < remainder {
				amt++
			}
			h.players[seat].Stack += amt
		}
	}
}

// orderFromLeftOfButton returns seats (a subset of h.seatOrder) ordered
// starting from the seat immediately left of the button, wrapping
// around the table.
func (h *Hand) orderFromLeftOfButton(seats []int) []int {
	set := make(map[int]bool, len(seats))
	for _, s := range seats {
		set[s] = true
	}
	n := len(h.seatOrder)
	startIdx := h.seatIndex(h.seatAfter(h.button))
	out := make([]int, 0, len(seats))
	for k := 0; k < n; k++ {
		seat := h.seatOrder[(startIdx+k)%n]
		if set[seat] {
			out = append(out, seat)
		}
	}
	return out
}
