package server

import (
	"testing"

	"github.com/whoAngeel/pokerhub/internal/protocol"
)

func env(t *testing.T, typ string, handID int, payload any) protocol.Envelope {
	t.Helper()
	e, err := protocol.NewEnvelope(typ, protocol.IntPtr(handID), payload)
	if err != nil {
		t.Fatalf("NewEnvelope(%s): %v", typ, err)
	}
	return e.WithSeq(1)
}

// TestProject_VisibilityTable is THE single visibility-filter test
// (docs/PROTOCOL.md §1): for every event type, it asserts what spectator
// and a non-owning player seat each get to see. This is the critical
// safety property of the whole server package -- a new, unlisted event
// type must fail closed (not delivered to a player) while spectator still
// sees everything, no exceptions.
func TestProject_VisibilityTable(t *testing.T) {
	cases := []struct {
		name          string
		typ           string
		payload       any
		wantSpectator bool
		wantOtherSeat bool // delivered to a player seat NOT the one the event is about
	}{
		{"hand_start public", protocol.TypeHandStart, protocol.HandStartPayload{HandID: 1}, true, true},
		{"blind public", protocol.TypeBlind, protocol.BlindPayload{Seat: 0}, true, true},
		{"street public", protocol.TypeStreet, protocol.StreetPayload{Street: "flop"}, true, true},
		{"thinking public", protocol.TypeThinking, protocol.ThinkingPayload{Seat: 1}, true, true},
		{"action public", protocol.TypeAction, protocol.ActionPayload{Seat: 1}, true, true},
		{"chat public", protocol.TypeChat, protocol.ChatPayload{Seat: 1, Say: "hola"}, true, true},
		{"showdown public", protocol.TypeShowdown, protocol.ShowdownPayload{}, true, true},
		{"pot_awarded public", protocol.TypePotAwarded, protocol.PotAwardedPayload{}, true, true},
		{"hand_end public", protocol.TypeHandEnd, protocol.HandEndPayload{HandID: 1}, true, true},
		{"rebuy public", protocol.TypeRebuy, protocol.RebuyPayload{Seat: 1}, true, true},
		{"hole_cards private to owner", protocol.TypeHoleCards, protocol.HoleCardsPayload{Seat: 2, Cards: []protocol.Card{"Ah"}}, true, false},
		{"reasoning spectator only", protocol.TypeReasoning, protocol.ReasoningPayload{Seat: 2}, true, false},
		{"memory_update spectator only", protocol.TypeMemoryUpdate, protocol.MemoryUpdatePayload{}, true, false},
		{"agent_error spectator only", protocol.TypeAgentError, protocol.AgentErrorPayload{Seat: 2}, true, false},
		{"unknown type fails closed for players", "totally_new_type", map[string]string{"x": "y"}, true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := env(t, tc.typ, 1, tc.payload)

			_, specOK := Project(e, Spectate())
			if specOK != tc.wantSpectator {
				t.Fatalf("spectator delivered=%v, want %v", specOK, tc.wantSpectator)
			}

			// seat 9 is never "the seat the event is about" in any case
			// above, so it exercises "a player who isn't involved".
			_, otherOK := Project(e, PlayerSeat(9))
			if otherOK != tc.wantOtherSeat {
				t.Fatalf("player:9 delivered=%v, want %v", otherOK, tc.wantOtherSeat)
			}
		})
	}

	// hole_cards/turn/snapshot need the OWNING seat checked too (that part
	// isn't a flat "never/always", it depends on whose seat it is).
	t.Run("hole_cards reaches its own seat", func(t *testing.T) {
		e := env(t, protocol.TypeHoleCards, 1, protocol.HoleCardsPayload{Seat: 2, Cards: []protocol.Card{"Ah", "Kd"}})
		got, ok := Project(e, PlayerSeat(2))
		if !ok {
			t.Fatal("owner seat should see its own hole_cards")
		}
		var p protocol.HoleCardsPayload
		if err := got.Decode(&p); err != nil || len(p.Cards) != 2 {
			t.Fatalf("owner should see full cards, got %+v err=%v", p, err)
		}
	})

	t.Run("turn is public but legal_actions is owner+spectator only", func(t *testing.T) {
		e := env(t, protocol.TypeTurn, 1, protocol.TurnPayload{
			Seat: 2, ToCall: 10, DeadlineMS: 5000,
			LegalActions: []protocol.LegalAction{{Type: "fold"}, {Type: "call", Amount: 10}},
		})

		got, ok := Project(e, PlayerSeat(9))
		if !ok {
			t.Fatal("turn is public: another seat should still receive it")
		}
		var p protocol.TurnPayload
		_ = got.Decode(&p)
		if len(p.LegalActions) != 0 {
			t.Fatalf("another seat must never see seat 2's legal_actions, got %+v", p.LegalActions)
		}

		got, ok = Project(e, PlayerSeat(2))
		if !ok {
			t.Fatal("owner seat should see turn")
		}
		_ = got.Decode(&p)
		if len(p.LegalActions) != 2 {
			t.Fatalf("owner should see its own legal_actions, got %+v", p)
		}
	})

	t.Run("snapshot filters hole cards and legal_actions per role", func(t *testing.T) {
		e := env(t, protocol.TypeSnapshot, 1, protocol.SnapshotPayload{
			ToAct: 1,
			Seats: []protocol.SnapshotSeat{
				{Seat: 0, Hole: []protocol.Card{"Ah", "Kd"}},
				{Seat: 1, Hole: []protocol.Card{"2s", "3s"}},
			},
			LegalActions: []protocol.LegalAction{{Type: "fold"}},
		})

		got, ok := Project(e, PlayerSeat(0))
		if !ok {
			t.Fatal("player:0 should see snapshot")
		}
		var p protocol.SnapshotPayload
		_ = got.Decode(&p)
		for _, s := range p.Seats {
			if s.Seat != 0 && s.Hole != nil {
				t.Fatalf("player:0 must never see seat %d's hole cards: %+v", s.Seat, s)
			}
			if s.Seat == 0 && len(s.Hole) != 2 {
				t.Fatalf("player:0 should see its own hole cards: %+v", s)
			}
		}
		if len(p.LegalActions) != 0 {
			t.Fatalf("player:0 must not see legal_actions for seat 1's turn, got %+v", p.LegalActions)
		}
	})
}
