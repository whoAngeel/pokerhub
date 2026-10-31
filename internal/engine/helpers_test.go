package engine

import "math/rand/v2"

// cs builds a []Card from short strings like "Ah", "Td".
func cs(ss ...string) []Card {
	out := make([]Card, len(ss))
	for i, s := range ss {
		out[i] = Card(s)
	}
	return out
}

func testRNG(seed uint64) *rand.Rand {
	return rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))
}

// newTestHand builds a Hand bypassing NewHand's dealing/shuffling, so
// pot-math tests can set exact hole cards, board and contributions.
func newTestHand(cfg HandConfig, button int, players map[int]*playerState) *Hand {
	h := &Hand{
		cfg:      cfg,
		button:   button,
		players:  players,
		street:   River,
		revealed: make(map[int][]Card),
		rng:      testRNG(1),
	}
	for seat := range players {
		h.seatOrder = append(h.seatOrder, seat)
	}
	// Keep seatOrder sorted for deterministic "left of button" ordering.
	for i := 1; i < len(h.seatOrder); i++ {
		for j := i; j > 0 && h.seatOrder[j-1] > h.seatOrder[j]; j-- {
			h.seatOrder[j-1], h.seatOrder[j] = h.seatOrder[j], h.seatOrder[j-1]
		}
	}
	return h
}

func p(stack int, status SeatStatus, hole []Card, totalInPot int) *playerState {
	return &playerState{
		Stack: stack, Status: status, Hole: hole, TotalInPot: totalInPot,
		DealtIn: true, CanRaise: true,
	}
}
