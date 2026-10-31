package engine

import (
	"reflect"
	"sort"
	"testing"
)

func TestSidePotsThreeWayDifferentStacks(t *testing.T) {
	// Seat 0 all-in for 100 total, seat 1 all-in for 300 total, seat 2
	// covers everything with 300. Board makes seat 2 the best hand
	// overall, seat 1 second best, seat 0 worst.
	board := cs("2c", "7d", "9h", "Jc", "4s")
	players := map[int]*playerState{
		0: p(0, AllInState, cs("3s", "3h"), 100), // pair of 3s: weakest
		1: p(0, AllInState, cs("9s", "9c"), 300), // trips 9s
		2: p(700, Active, cs("Ks", "Kh"), 300),   // pair of Ks: beats trips? no -- fix below
	}
	h := newTestHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, 0, players)
	h.board = board

	pots := h.buildPots()
	if len(pots) != 2 {
		t.Fatalf("expected 2 pots (main+side), got %d: %+v", len(pots), pots)
	}
	if pots[0].Amount != 300 { // (100-0)*3
		t.Fatalf("main pot should be 300, got %d", pots[0].Amount)
	}
	if pots[1].Amount != 400 { // (300-100)*2
		t.Fatalf("side pot should be 400, got %d", pots[1].Amount)
	}
	mainElig := append([]int{}, pots[0].Eligible...)
	sort.Ints(mainElig)
	if !reflect.DeepEqual(mainElig, []int{0, 1, 2}) {
		t.Fatalf("main pot eligible should be all 3, got %v", mainElig)
	}
	sideElig := append([]int{}, pots[1].Eligible...)
	sort.Ints(sideElig)
	if !reflect.DeepEqual(sideElig, []int{1, 2}) {
		t.Fatalf("side pot eligible should be seats 1,2, got %v", sideElig)
	}

	h.awardPots(pots)
	// Trips (9s, seat1) beats pair of Kings (seat2) beats pair of 3s (seat0).
	if len(pots[0].Winners) != 1 || pots[0].Winners[0] != 1 {
		t.Fatalf("main pot should be won by seat 1 (trips), got %+v", pots[0].Winners)
	}
	if len(pots[1].Winners) != 1 || pots[1].Winners[0] != 1 {
		t.Fatalf("side pot should be won by seat 1 (trips), got %+v", pots[1].Winners)
	}
	if players[1].Stack != 700 { // 300 (main) + 400 (side)
		t.Fatalf("seat1 should collect everything (700), got %d", players[1].Stack)
	}
	if players[0].Stack != 0 || players[2].Stack != 700 {
		t.Fatalf("unexpected stacks: seat0=%d seat2=%d", players[0].Stack, players[2].Stack)
	}
}

func TestSidePotsFoldedContributor(t *testing.T) {
	// Seat 1 called 100 then folded; its chips stay in the pot but it's
	// not eligible to win.
	board := cs("2c", "7d", "9h", "Jc", "4s")
	players := map[int]*playerState{
		0: p(0, AllInState, cs("3s", "3h"), 100),
		1: p(0, Folded, cs("5c", "6c"), 100),
		2: p(700, Active, cs("Ks", "Kh"), 100),
	}
	h := newTestHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, 0, players)
	h.board = board

	pots := h.buildPots()
	if len(pots) != 1 {
		t.Fatalf("expected a single pot, got %d: %+v", len(pots), pots)
	}
	if pots[0].Amount != 300 {
		t.Fatalf("pot should include the folded contribution: want 300 got %d", pots[0].Amount)
	}
	elig := append([]int{}, pots[0].Eligible...)
	sort.Ints(elig)
	if !reflect.DeepEqual(elig, []int{0, 2}) {
		t.Fatalf("folded seat 1 must not be eligible, got %v", elig)
	}
	h.awardPots(pots)
	if players[2].Stack != 1000 {
		t.Fatalf("seat2 (pair of kings) should win the whole 300-chip pot: got stack %d", players[2].Stack)
	}
}

func TestSplitPotOddChip(t *testing.T) {
	// Two players tie exactly; pot has an odd chip that must go to the
	// seat left of the button first.
	board := cs("2c", "7d", "9h", "Jc", "4s")
	players := map[int]*playerState{
		0: p(0, AllInState, cs("3s", "3h"), 51),
		1: p(0, AllInState, cs("3c", "3d"), 51),
		2: p(898, Active, cs("Kh", "2h"), 51), // weakest hand, folds out of contention via rank
	}
	h := newTestHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, 0, players)
	h.board = board
	pots := h.buildPots()
	if len(pots) != 1 || pots[0].Amount != 153 {
		t.Fatalf("expected one 153-chip pot, got %+v", pots)
	}
	h.awardPots(pots)
	// seat0 and seat1 tie (same pair rank, board kickers identical);
	// seat2 has a worse hand (king-high, no pair) so only 0 and 1 split.
	winners := pots[0].Winners
	sort.Ints(winners)
	if !reflect.DeepEqual(winners, []int{0, 1}) {
		t.Fatalf("expected seats 0 and 1 to split, got %v", winners)
	}
	// seatAfter(button=0) = seat1 is "left of the button", so the odd
	// chip (153 is odd, 76+77) goes to seat1 first.
	total := players[0].Stack + players[1].Stack
	if total != 153 {
		t.Fatalf("split total should be 153, got %d", total)
	}
	if players[1].Stack != 77 || players[0].Stack != 76 {
		t.Fatalf("odd chip should go to seat1 (left of button): seat0=%d seat1=%d", players[0].Stack, players[1].Stack)
	}
}

func TestUncalledBetReturn(t *testing.T) {
	players := map[int]*playerState{
		0: p(900, Active, cs("Ah", "Kh"), 100), // bet 100, nobody called the extra
		1: p(0, Folded, cs("2c", "3c"), 20),
	}
	h := newTestHand(HandConfig{HandID: 1, SmallBlind: 5, BigBlind: 10}, 0, players)
	h.returnUncalledBet()
	if players[0].TotalInPot != 20 {
		t.Fatalf("seat0's contribution should be capped at the next-highest (20), got %d", players[0].TotalInPot)
	}
	if players[0].Stack != 980 {
		t.Fatalf("seat0 should get the uncalled 80 back: want stack 980 got %d", players[0].Stack)
	}
}
