package memory

import (
	"testing"

	"github.com/whoAngeel/pokerhub/internal/engine"
)

func act(seat int, typ engine.ActionType) engine.Event {
	return engine.Event{Kind: engine.EvAction, Seat: seat, Action: engine.Action{Type: typ, Amount: 10}}
}

func TestTracker_HandsAndVPIP(t *testing.T) {
	tr := NewTracker()

	events := []engine.Event{
		{Kind: engine.EvHandStart},
		{Kind: engine.EvHoleCards, Seat: 0},
		{Kind: engine.EvHoleCards, Seat: 1},
		{Kind: engine.EvHoleCards, Seat: 2},
		// seat 0 raises preflop (VPIP + PFR), seat 1 calls (VPIP only), seat 2 folds (neither).
		act(0, engine.Raise),
		act(1, engine.Call),
		act(2, engine.Fold),
		{Kind: engine.EvHandEnd},
	}
	for _, ev := range events {
		tr.Record(ev)
	}

	s0 := tr.Stats(0)
	if s0.Hands != 1 || s0.VPIP != 1 || s0.PFR != 1 {
		t.Fatalf("seat0: %+v", s0)
	}
	s1 := tr.Stats(1)
	if s1.Hands != 1 || s1.VPIP != 1 || s1.PFR != 0 {
		t.Fatalf("seat1: %+v", s1)
	}
	s2 := tr.Stats(2)
	if s2.Hands != 1 || s2.VPIP != 0 || s2.PFR != 0 {
		t.Fatalf("seat2: %+v", s2)
	}
}

func TestTracker_VPIP_PFR_CountOncePerHand(t *testing.T) {
	tr := NewTracker()
	events := []engine.Event{
		{Kind: engine.EvHandStart},
		{Kind: engine.EvHoleCards, Seat: 0},
		act(0, engine.Raise),
		act(0, engine.Raise), // re-raised later in the same preflop street: must not double count
		{Kind: engine.EvHandEnd},
	}
	for _, ev := range events {
		tr.Record(ev)
	}
	s := tr.Stats(0)
	if s.Hands != 1 || s.VPIP != 1 || s.PFR != 1 {
		t.Fatalf("expected single count, got %+v", s)
	}
}

func TestTracker_AF(t *testing.T) {
	tr := NewTracker()
	events := []engine.Event{
		{Kind: engine.EvHandStart},
		{Kind: engine.EvHoleCards, Seat: 0},
		act(0, engine.Bet),
		{Kind: engine.EvStreet, Street: engine.Flop},
		act(0, engine.Raise),
		act(0, engine.Call),
		{Kind: engine.EvHandEnd},
	}
	for _, ev := range events {
		tr.Record(ev)
	}
	s := tr.Stats(0)
	// 2 bets/raises, 1 call -> AF = 2/1 = 2
	if s.AF != 2 {
		t.Fatalf("expected AF=2, got %v (%+v)", s.AF, s)
	}
}

func TestTracker_AFZeroCallsIsZero(t *testing.T) {
	tr := NewTracker()
	tr.Record(engine.Event{Kind: engine.EvHandStart})
	tr.Record(engine.Event{Kind: engine.EvHoleCards, Seat: 0})
	tr.Record(act(0, engine.Bet))
	tr.Record(engine.Event{Kind: engine.EvHandEnd})

	s := tr.Stats(0)
	if s.AF != 0 {
		t.Fatalf("expected AF=0 with no calls yet, got %v", s.AF)
	}
}

func TestTracker_RiverBluffShown(t *testing.T) {
	tr := NewTracker()
	events := []engine.Event{
		{Kind: engine.EvHandStart},
		{Kind: engine.EvHoleCards, Seat: 0},
		{Kind: engine.EvHoleCards, Seat: 1},
		{Kind: engine.EvStreet, Street: engine.Flop},
		{Kind: engine.EvStreet, Street: engine.Turn},
		{Kind: engine.EvStreet, Street: engine.River},
		act(0, engine.Bet), // seat 0 bets the river
		{Kind: engine.EvShowdown, Reveals: []engine.Reveal{{Seat: 0}, {Seat: 1}}},
		{Kind: engine.EvPotAwarded, Pots: []engine.Pot{{Amount: 100, Winners: []int{1}}}}, // seat 0 loses
		{Kind: engine.EvHandEnd},
	}
	for _, ev := range events {
		tr.Record(ev)
	}
	s0 := tr.Stats(0)
	if s0.RiverBluffsShown != 1 {
		t.Fatalf("expected 1 river bluff shown, got %+v", s0)
	}
	s1 := tr.Stats(1)
	if s1.RiverBluffsShown != 0 {
		t.Fatalf("winner must not count as a bluff: %+v", s1)
	}
}

func TestTracker_RiverBluffNotShownWhenWon(t *testing.T) {
	tr := NewTracker()
	events := []engine.Event{
		{Kind: engine.EvHandStart},
		{Kind: engine.EvHoleCards, Seat: 0},
		{Kind: engine.EvHoleCards, Seat: 1},
		{Kind: engine.EvStreet, Street: engine.Flop},
		{Kind: engine.EvStreet, Street: engine.Turn},
		{Kind: engine.EvStreet, Street: engine.River},
		act(0, engine.Bet),
		{Kind: engine.EvShowdown, Reveals: []engine.Reveal{{Seat: 0}, {Seat: 1}}},
		{Kind: engine.EvPotAwarded, Pots: []engine.Pot{{Amount: 100, Winners: []int{0}}}}, // seat 0 wins this time
		{Kind: engine.EvHandEnd},
	}
	for _, ev := range events {
		tr.Record(ev)
	}
	s0 := tr.Stats(0)
	if s0.RiverBluffsShown != 0 {
		t.Fatalf("a winning river bet must not count as a bluff: %+v", s0)
	}
}

func TestTracker_StatsMapAndUnseenSeat(t *testing.T) {
	tr := NewTracker()
	if s := tr.Stats(42); s.Hands != 0 {
		t.Fatalf("unseen seat should be zero value, got %+v", s)
	}
	tr.Record(engine.Event{Kind: engine.EvHandStart})
	tr.Record(engine.Event{Kind: engine.EvHoleCards, Seat: 0})
	tr.Record(engine.Event{Kind: engine.EvHandEnd})

	m := tr.StatsMap()
	if len(m) != 1 || m[0].Hands != 1 {
		t.Fatalf("unexpected StatsMap: %+v", m)
	}
}
