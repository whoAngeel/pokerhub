package engine

import "testing"

func threeSeats(stacks [3]int) []SeatState {
	return []SeatState{
		{Seat: 0, Stack: stacks[0], Status: Active},
		{Seat: 1, Stack: stacks[1], Status: Active},
		{Seat: 2, Stack: stacks[2], Status: Active},
	}
}

func TestHeadsUpOrder(t *testing.T) {
	seats := []SeatState{
		{Seat: 0, Stack: 1000, Status: Active},
		{Seat: 1, Stack: 1000, Status: Active},
	}
	h, _ := NewHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, seats, 0, testRNG(1))

	seat, ok := h.ToAct()
	if !ok || seat != 0 {
		t.Fatalf("heads-up: button (seat 0) should act first preflop, got seat=%d ok=%v", seat, ok)
	}
	if _, err := h.Apply(0, Action{Type: Call}); err != nil {
		t.Fatalf("apply call: %v", err)
	}
	seat, ok = h.ToAct()
	if !ok || seat != 1 {
		t.Fatalf("heads-up: BB (seat 1) should act next, got seat=%d ok=%v", seat, ok)
	}
	if _, err := h.Apply(1, Action{Type: Check}); err != nil {
		t.Fatalf("apply check: %v", err)
	}
	// Postflop: BB (seat 1) acts first in heads-up.
	seat, ok = h.ToAct()
	if !ok || seat != 1 {
		t.Fatalf("heads-up: BB (seat 1) should act first postflop, got seat=%d ok=%v", seat, ok)
	}
}

func TestThreeWayPreflopOrder(t *testing.T) {
	seats := threeSeats([3]int{1000, 1000, 1000})
	h, _ := NewHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, seats, 0, testRNG(1))
	// button=0, SB=1, BB=2, first to act preflop = seat after BB = 0.
	seat, ok := h.ToAct()
	if !ok || seat != 0 {
		t.Fatalf("expected button (seat 0) first to act preflop in 3-way, got %d", seat)
	}
}

func TestBlindsShortStack(t *testing.T) {
	// SB seat only has 3 chips, less than SmallBlind(5): posts all-in for less.
	seats := []SeatState{
		{Seat: 0, Stack: 1000, Status: Active},
		{Seat: 1, Stack: 3, Status: Active},
		{Seat: 2, Stack: 1000, Status: Active},
	}
	_, events := NewHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, seats, 0, testRNG(1))
	var sbEvent Event
	found := false
	for _, e := range events {
		if e.Kind == EvBlind && e.Seat == 1 {
			sbEvent = e
			found = true
		}
	}
	if !found {
		t.Fatalf("no blind event for seat 1")
	}
	if sbEvent.Action.Amount != 3 {
		t.Fatalf("short SB should post all its 3 chips, got %d", sbEvent.Action.Amount)
	}
	if sbEvent.StackAfter != 0 {
		t.Fatalf("short SB stack should be 0 after posting, got %d", sbEvent.StackAfter)
	}
}

func TestCheckOnlyLegalWhenNoBet(t *testing.T) {
	seats := threeSeats([3]int{1000, 1000, 1000})
	h, _ := NewHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, seats, 0, testRNG(1))
	seat, _ := h.ToAct() // seat 0, facing the BB, to_call=10
	acts := h.LegalActions(seat)
	if _, ok := findLegal(acts, Check); ok {
		t.Fatalf("check should not be legal while facing a bet")
	}
	if la, ok := findLegal(acts, Call); !ok || la.Amount != 10 {
		t.Fatalf("expected call for 10, got %+v ok=%v", la, ok)
	}
}

func TestIllegalActionDoesNotMutate(t *testing.T) {
	seats := threeSeats([3]int{1000, 1000, 1000})
	h, _ := NewHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, seats, 0, testRNG(1))
	before := h.Public()

	seat, _ := h.ToAct()
	if _, err := h.Apply(seat, Action{Type: Check}); err == nil {
		t.Fatalf("check should be illegal while facing a bet")
	}
	if _, err := h.Apply((seat+1)%3, Action{Type: Call}); err == nil {
		t.Fatalf("acting out of turn should error")
	}
	if _, err := h.Apply(seat, Action{Type: Raise, Amount: 1}); err == nil {
		t.Fatalf("raise below minimum should error")
	}

	after := h.Public()
	if before.Pot != after.Pot {
		t.Fatalf("pot changed after illegal actions: before=%d after=%d", before.Pot, after.Pot)
	}
	for i := range before.Seats {
		if before.Seats[i] != after.Seats[i] {
			t.Fatalf("seat state changed after illegal action: before=%+v after=%+v", before.Seats[i], after.Seats[i])
		}
	}
}

func TestMinRaiseComputation(t *testing.T) {
	seats := threeSeats([3]int{1000, 1000, 1000})
	h, _ := NewHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, seats, 0, testRNG(1))
	seat, _ := h.ToAct() // seat 0, currentBet=10 (BB), lastFullRaiseSize=10
	la, ok := findLegal(h.LegalActions(seat), Raise)
	if !ok {
		t.Fatalf("raise should be legal")
	}
	if la.Min != 20 { // currentBet(10) + lastFullRaiseSize(10)
		t.Fatalf("expected min raise-to 20, got %d", la.Min)
	}

	// Seat 0 raises to 30 (a full raise: +20 over the 10 BB).
	if _, err := h.Apply(seat, Action{Type: Raise, Amount: 30}); err != nil {
		t.Fatalf("raise: %v", err)
	}
	// Seat 1 (SB) now faces a raise to 30; min re-raise = 30+20=50.
	la, ok = findLegal(h.LegalActions(1), Raise)
	if !ok {
		t.Fatalf("re-raise should be legal for seat 1")
	}
	if la.Min != 50 {
		t.Fatalf("expected min re-raise-to 50, got %d", la.Min)
	}
}

func TestIncompleteAllInDoesNotReopen(t *testing.T) {
	// Seat 0 bets big postflop-style via preflop raise, seat1 calls,
	// seat2 (short) goes all-in for less than a full raise. Seat0/seat1
	// already acted and must be restricted to call/fold only.
	seats := []SeatState{
		{Seat: 0, Stack: 1000, Status: Active},
		{Seat: 1, Stack: 1000, Status: Active},
		{Seat: 2, Stack: 50, Status: Active}, // BB, short stack
	}
	h, _ := NewHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, seats, 0, testRNG(1))
	// Preflop order (button=0): first to act = seat0 (button/UTG).
	if _, err := h.Apply(0, Action{Type: Raise, Amount: 40}); err != nil { // full raise to 40 (+30, full since 30>=10)
		t.Fatalf("seat0 raise: %v", err)
	}
	if _, err := h.Apply(1, Action{Type: Call}); err != nil { // seat1 calls 40, ActedThisStreet=true
		t.Fatalf("seat1 call: %v", err)
	}
	// seat2 (BB) already posted 10, leaving 40 behind; going all-in for
	// that 40 brings its total to 50 — above the current bet (40) but
	// below the full-raise minimum (40 + lastFullRaiseSize(30) = 70),
	// so it's an incomplete raise.
	seat, ok := h.ToAct()
	if !ok || seat != 2 {
		t.Fatalf("expected seat2 to act, got %d ok=%v", seat, ok)
	}
	allIn, ok := findLegal(h.LegalActions(2), AllIn)
	if !ok {
		t.Fatalf("all_in should be legal for seat2")
	}
	if _, err := h.Apply(2, Action{Type: allIn.Type}); err != nil {
		t.Fatalf("seat2 all-in: %v", err)
	}

	// Now seat0 and seat1 already acted this round and face an
	// incomplete raise (25 < 70): they must not be offered Raise.
	seat, ok = h.ToAct()
	if !ok || seat != 0 {
		t.Fatalf("expected seat0 to act again, got %d ok=%v", seat, ok)
	}
	if _, raiseOK := findLegal(h.LegalActions(0), Raise); raiseOK {
		t.Fatalf("seat0 should not be allowed to raise after an incomplete all-in")
	}
	if _, shoveOK := findLegal(h.LegalActions(0), AllIn); shoveOK {
		t.Fatalf("seat0 should not be allowed to shove (a raise) after an incomplete all-in")
	}
	if _, err := h.Apply(0, Action{Type: Check}); err == nil {
		t.Fatalf("seat0 cannot check: it still owes a call behind the incomplete raise")
	}
	la, ok := findLegal(h.LegalActions(0), Call)
	if !ok {
		t.Fatalf("seat0 should still be able to call")
	}
	if _, err := h.Apply(0, Action{Type: la.Type}); err != nil {
		t.Fatalf("seat0 call: %v", err)
	}

	// seat1 is also restricted to call/fold only.
	if _, raiseOK := findLegal(h.LegalActions(1), Raise); raiseOK {
		t.Fatalf("seat1 should not be allowed to raise after an incomplete all-in")
	}
}

func TestBetOnlyLegalWithNoCurrentBet(t *testing.T) {
	seats := threeSeats([3]int{1000, 1000, 1000})
	h, _ := NewHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, seats, 0, testRNG(1))
	// Drive to flop with everyone calling/checking.
	for !h.Done() && h.Public().Street == Preflop {
		seat, ok := h.ToAct()
		if !ok {
			break
		}
		acts := h.LegalActions(seat)
		if la, ok := findLegal(acts, Check); ok {
			h.Apply(seat, Action{Type: la.Type})
			continue
		}
		la, _ := findLegal(acts, Call)
		h.Apply(seat, Action{Type: la.Type})
	}
	if h.Public().Street != Flop {
		t.Fatalf("expected to reach flop, at %s", h.Public().Street)
	}
	seat, _ := h.ToAct()
	acts := h.LegalActions(seat)
	if _, ok := findLegal(acts, Bet); !ok {
		t.Fatalf("bet should be legal on a fresh street with no bets yet")
	}
	if _, ok := findLegal(acts, Raise); ok {
		t.Fatalf("raise should not be legal before any bet exists")
	}
}
