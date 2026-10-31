package engine

import (
	"encoding/json"
	"math/rand/v2"
	"sort"
	"strings"
	"testing"
)

// randomAction picks a uniformly random legal action, and a random
// in-range amount for Bet/Raise.
func randomAction(rng *rand.Rand, acts []LegalAction) Action {
	la := acts[rng.IntN(len(acts))]
	switch la.Type {
	case Bet, Raise:
		amt := la.Min
		if la.Max > la.Min {
			amt = la.Min + rng.IntN(la.Max-la.Min+1)
		}
		return Action{Type: la.Type, Amount: amt}
	default:
		return Action{Type: la.Type}
	}
}

func TestFoldToWin(t *testing.T) {
	seats := threeSeats([3]int{1000, 1000, 1000})
	h, _ := NewHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, seats, 0, testRNG(1))

	seat, _ := h.ToAct() // seat0 (button/UTG)
	if _, err := h.Apply(seat, Action{Type: Raise, Amount: 40}); err != nil {
		t.Fatalf("raise: %v", err)
	}
	// seat1 (SB) folds.
	evs, err := h.Apply(1, Action{Type: Fold})
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	for _, e := range evs {
		if e.Kind == EvShowdown {
			t.Fatalf("no showdown should happen while 2 players remain")
		}
	}
	// seat2 (BB) folds -> only seat0 remains, hand ends uncontested.
	evs, err = h.Apply(2, Action{Type: Fold})
	if err != nil {
		t.Fatalf("fold: %v", err)
	}
	if !h.Done() {
		t.Fatalf("hand should be done after fold-to-one")
	}
	var sawPotAwarded, sawHandEnd, sawShowdown bool
	var finalStacks map[int]int
	for _, e := range evs {
		switch e.Kind {
		case EvShowdown:
			sawShowdown = true
		case EvPotAwarded:
			sawPotAwarded = true
			if len(e.Pots) != 1 || e.Pots[0].Winners[0] != 0 {
				t.Fatalf("expected seat0 to win the only pot, got %+v", e.Pots)
			}
		case EvHandEnd:
			sawHandEnd = true
			finalStacks = e.Stacks
		}
	}
	if sawShowdown {
		t.Fatalf("fold-to-one must never show a showdown event")
	}
	if !sawPotAwarded || !sawHandEnd {
		t.Fatalf("expected pot_awarded and hand_end, got events: %+v", evs)
	}
	total := 0
	for _, s := range finalStacks {
		total += s
	}
	if total != 3000 {
		t.Fatalf("chips not conserved on fold-to-win: total=%d", total)
	}
	if finalStacks[0] <= 1000 {
		t.Fatalf("winner should have more than their starting stack: %d", finalStacks[0])
	}
}

func TestRunOutAllInPreflop(t *testing.T) {
	seats := []SeatState{
		{Seat: 0, Stack: 1000, Status: Active},
		{Seat: 1, Stack: 1000, Status: Active},
	}
	h, _ := NewHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, seats, 0, testRNG(2))

	seat, _ := h.ToAct() // heads-up: button (seat0) acts first preflop
	if _, err := h.Apply(seat, Action{Type: AllIn}); err != nil {
		t.Fatalf("all-in: %v", err)
	}
	other := 1 - seat
	evs, err := h.Apply(other, Action{Type: AllIn})
	if err != nil {
		t.Fatalf("call all-in: %v", err)
	}

	if !h.Done() {
		t.Fatalf("hand should resolve immediately once both are all-in preflop")
	}
	kinds := map[EventKind]int{}
	for _, e := range evs {
		kinds[e.Kind]++
	}
	if kinds[EvStreet] != 3 {
		t.Fatalf("expected 3 street events (flop,turn,river), got %d: %+v", kinds[EvStreet], evs)
	}
	if kinds[EvShowdown] != 1 || kinds[EvPotAwarded] != 1 || kinds[EvHandEnd] != 1 {
		t.Fatalf("expected exactly one showdown/pot_awarded/hand_end, got %+v", kinds)
	}
	pub := h.Public()
	if len(pub.Board) != 5 {
		t.Fatalf("board should be fully dealt, got %d cards", len(pub.Board))
	}
	total := 0
	for _, s := range pub.Seats {
		total += s.Stack
	}
	if total != 2000 {
		t.Fatalf("chips not conserved: %d", total)
	}
}

func TestDeterminism(t *testing.T) {
	run := func(seed uint64) []Event {
		seats := threeSeats([3]int{500, 300, 800})
		h, events := NewHand(HandConfig{HandID: 7, SmallBlind: 5, BigBlind: 10}, seats, 1, testRNG(seed))
		for !h.Done() {
			seat, ok := h.ToAct()
			if !ok {
				break
			}
			acts := h.LegalActions(seat)
			// Deterministic, scripted policy: prefer check, then call,
			// then whatever's first (never a randomized choice).
			var a Action
			if la, ok := findLegal(acts, Check); ok {
				a = Action{Type: la.Type}
			} else if la, ok := findLegal(acts, Call); ok {
				a = Action{Type: la.Type}
			} else {
				a = Action{Type: acts[0].Type}
			}
			evs, err := h.Apply(seat, a)
			if err != nil {
				t.Fatalf("apply: %v", err)
			}
			events = append(events, evs...)
		}
		return events
	}

	a := run(42)
	b := run(42)
	if len(a) != len(b) {
		t.Fatalf("event stream length differs: %d vs %d", len(a), len(b))
	}
	for i := range a {
		aj, _ := json.Marshal(a[i])
		bj, _ := json.Marshal(b[i])
		if string(aj) != string(bj) {
			t.Fatalf("event %d differs:\n  a=%s\n  b=%s", i, aj, bj)
		}
	}

	c := run(43)
	same := len(a) == len(c)
	if same {
		for i := range a {
			aj, _ := json.Marshal(a[i])
			cj, _ := json.Marshal(c[i])
			if string(aj) != string(cj) {
				same = false
				break
			}
		}
	}
	if same {
		t.Fatalf("different seeds produced an identical event stream (seed not actually used)")
	}
}

// leakCheck marshals ViewFor(viewer) and fails the test if it contains
// any other dealt-in seat's hole card.
func leakCheck(t *testing.T, h *Hand, viewer int) {
	t.Helper()
	v := h.ViewFor(viewer)
	buf, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal ViewFor(%d): %v", viewer, err)
	}
	text := string(buf)
	for _, seat := range h.seatOrder {
		if seat == viewer {
			continue
		}
		for _, c := range h.players[seat].Hole {
			needle := `"` + string(c) + `"`
			if strings.Contains(text, needle) {
				t.Fatalf("ViewFor(%d) leaked seat %d's hole card %s: %s", viewer, seat, c, text)
			}
		}
	}
}

func randomDistinctSeats(rng *rand.Rand, n int) []int {
	pool := make([]int, 10)
	for i := range pool {
		pool[i] = i
	}
	rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	out := append([]int{}, pool[:n]...)
	sort.Ints(out)
	return out
}

func TestInfoLeak(t *testing.T) {
	master := testRNG(999)
	for i := 0; i < 1000; i++ {
		n := 2 + master.IntN(4) // 2..5 players
		seatIDs := randomDistinctSeats(master, n)
		button := seatIDs[master.IntN(n)]
		seats := make([]SeatState, n)
		for j, id := range seatIDs {
			seats[j] = SeatState{Seat: id, Stack: 10 + master.IntN(2000), Status: Active}
		}
		dealSeed := master.Uint64()
		h, _ := NewHand(HandConfig{HandID: i, SmallBlind: 5, BigBlind: 10}, seats, button, testRNG(dealSeed))

		for _, viewer := range seatIDs {
			if !h.Done() {
				leakCheck(t, h, viewer)
			}
		}
		for step := 0; step < 400 && !h.Done(); step++ {
			seat, ok := h.ToAct()
			if !ok {
				break
			}
			acts := h.LegalActions(seat)
			if len(acts) == 0 {
				t.Fatalf("hand %d: seat %d to act but no legal actions", i, seat)
			}
			a := randomAction(master, acts)
			if _, err := h.Apply(seat, a); err != nil {
				t.Fatalf("hand %d: apply %+v for seat %d: %v", i, a, seat, err)
			}
			if !h.Done() {
				for _, viewer := range seatIDs {
					leakCheck(t, h, viewer)
				}
			}
		}
		if !h.Done() {
			t.Fatalf("hand %d did not terminate", i)
		}
	}
}

func TestInvariantFuzz(t *testing.T) {
	master := testRNG(31337)
	const hands = 10000
	for i := 0; i < hands; i++ {
		n := 2 + master.IntN(4) // 2..5 players
		seatIDs := randomDistinctSeats(master, n)
		button := seatIDs[master.IntN(n)]
		seats := make([]SeatState, n)
		startSum := 0
		for j, id := range seatIDs {
			stack := 1 + master.IntN(300) // small stacks: push toward all-ins/side pots
			seats[j] = SeatState{Seat: id, Stack: stack, Status: Active}
			startSum += stack
		}
		dealSeed := master.Uint64()
		h, _ := NewHand(HandConfig{HandID: i, SmallBlind: 5, BigBlind: 10}, seats, button, testRNG(dealSeed))

		for _, seat := range seatIDs {
			if h.players[seat].Stack < 0 {
				t.Fatalf("hand %d: negative stack right after dealing, seat %d", i, seat)
			}
		}

		terminated := false
		for step := 0; step < 2000; step++ {
			if h.Done() {
				terminated = true
				break
			}
			seat, ok := h.ToAct()
			if !ok {
				t.Fatalf("hand %d: not done but no seat to act", i)
			}
			acts := h.LegalActions(seat)
			if len(acts) == 0 {
				t.Fatalf("hand %d: seat %d to act but no legal actions", i, seat)
			}
			a := randomAction(master, acts)
			if _, err := h.Apply(seat, a); err != nil {
				t.Fatalf("hand %d: apply %+v for seat %d: %v", i, a, seat, err)
			}
			for _, s := range seatIDs {
				if h.players[s].Stack < 0 {
					t.Fatalf("hand %d step %d: negative stack, seat %d", i, step, s)
				}
			}
		}
		if !terminated {
			if h.Done() {
				terminated = true
			} else {
				t.Fatalf("hand %d did not terminate within step budget", i)
			}
		}

		endSum := 0
		for _, seat := range seatIDs {
			endSum += h.players[seat].Stack
		}
		if endSum != startSum {
			t.Fatalf("hand %d: chips not conserved: start=%d end=%d", i, startSum, endSum)
		}

		potsSum := 0
		for _, pot := range h.pots {
			potsSum += pot.Amount
		}
		contribSum := 0
		for _, seat := range seatIDs {
			contribSum += h.players[seat].TotalInPot
		}
		if potsSum != contribSum {
			t.Fatalf("hand %d: pots sum (%d) != contributions (%d)", i, potsSum, contribSum)
		}
	}
}
