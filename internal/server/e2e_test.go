package server

import (
	"context"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/whoAngeel/pokerhub/internal/agent"
	"github.com/whoAngeel/pokerhub/internal/llm"
	"github.com/whoAngeel/pokerhub/internal/player"
	"github.com/whoAngeel/pokerhub/internal/protocol"
	"github.com/whoAngeel/pokerhub/internal/table"
)

func e2ePersona(name string) agent.Persona {
	return agent.Persona{
		ID: strings.ToLower(name), Name: name, Temperature: 0.5,
		Reasoning: agent.ReasoningShort, Voice: "voz", Strategy: "estrategia",
	}
}

// buildE2ESession wires 2 rulebots + 1 mock-backed LLMAgent into a real
// table.Session whose sink is a real Hub -- the whole stack except the
// network/GPU (mock provider, no real LLM call).
func buildE2ESession(t *testing.T) (*table.Session, *Hub, PublicConfig) {
	t.Helper()
	pub := PublicConfig{
		SmallBlind: 5, BigBlind: 10, BuyIn: 500,
		Seats: []PublicConfigSeat{
			{Seat: 0, Name: "Bot0", Kind: "rulebot"},
			{Seat: 1, Name: "Bot1", Kind: "rulebot"},
			{Seat: 2, Name: "LLM2", Kind: "llm", PersonaID: protocol.StringPtr("llm2"), Model: protocol.StringPtr("mock-model")},
		},
	}
	hub := NewHub(pub.TableStateSeats(), 2000, 0)

	players := []player.Player{
		player.NewRuleBot("Bot0"),
		player.NewRuleBot("Bot1"),
		agent.New("LLM2", e2ePersona("LLM2"), llm.NewMock(), "mock", "mock-model"),
	}
	tcfg := table.Config{SmallBlind: 5, BigBlind: 10, BuyIn: 500, Seed: 7, DecisionTimeoutMS: 2000, EquityIters: 50}
	sess, err := table.NewSession(tcfg, players, hub)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return sess, hub, pub
}

// dialWS connects to srv's /ws endpoint with the given role/seat query and
// returns the raw websocket.Conn.
func dialWS(t *testing.T, srv *httptest.Server, query string) *websocket.Conn {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parse URL: %v", err)
	}
	u.Scheme = "ws"
	u.Path = "/ws"
	u.RawQuery = query

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, u.String(), nil)
	if err != nil {
		t.Fatalf("dial %s: %v", u.String(), err)
	}
	return c
}

// readUntil reads envelopes from c until pred returns true for one of them
// (returned), or deadline elapses (fails the test), collecting everything
// seen along the way into *all.
func readUntil(t *testing.T, c *websocket.Conn, all *[]protocol.Envelope, pred func(protocol.Envelope) bool, deadline time.Duration) protocol.Envelope {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), deadline)
	defer cancel()
	for {
		var env protocol.Envelope
		if err := wsjson.Read(ctx, c, &env); err != nil {
			t.Fatalf("read: %v (collected %d envelopes: %+v)", err, len(*all), *all)
		}
		*all = append(*all, env)
		if pred(env) {
			return env
		}
	}
}

func TestE2E_SpectatorAndPlayerVisibility(t *testing.T) {
	sess, hub, pub := buildE2ESession(t)
	app := NewApp(hub, sess, pub, false, "")
	srv := httptest.NewServer(app)
	defer srv.Close()

	specConn := dialWS(t, srv, "role=spectator")
	defer specConn.Close(websocket.StatusNormalClosure, "")
	playerConn := dialWS(t, srv, "role=player&seat=2")
	defer playerConn.Close(websocket.StatusNormalClosure, "")

	// Both connections must see a snapshot FIRST (PROTOCOL.md §2).
	var specAll, playerAll []protocol.Envelope
	specFirst := readUntil(t, specConn, &specAll, func(protocol.Envelope) bool { return true }, 5*time.Second)
	playerFirst := readUntil(t, playerConn, &playerAll, func(protocol.Envelope) bool { return true }, 5*time.Second)
	if specFirst.Type != protocol.TypeSnapshot {
		t.Fatalf("spectator's first message = %s, want snapshot", specFirst.Type)
	}
	if playerFirst.Type != protocol.TypeSnapshot {
		t.Fatalf("player's first message = %s, want snapshot", playerFirst.Type)
	}

	// Now run 3 hands to completion.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- sess.Run(ctx, 3) }()

	// Collect from spectator until we've seen 3 hand_end events.
	handEnds := 0
	readUntil(t, specConn, &specAll, func(e protocol.Envelope) bool {
		if e.Type == protocol.TypeHandEnd {
			handEnds++
		}
		return handEnds >= 3
	}, 10*time.Second)
	cancel()
	<-runDone

	// Drain whatever else is already buffered for the player connection
	// (non-blocking-ish: short deadline, best effort) so we have a
	// representative sample to assert against.
	drainCtx, drainCancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer drainCancel()
	for {
		var env protocol.Envelope
		if err := wsjson.Read(drainCtx, playerConn, &env); err != nil {
			break
		}
		playerAll = append(playerAll, env)
	}

	// --- Spectator assertions ---
	var sawReasoning, sawAllHoleCardsSeats bool
	holeSeatsSeenBySpectator := map[int]bool{}
	for _, e := range specAll {
		switch e.Type {
		case protocol.TypeReasoning:
			sawReasoning = true
		case protocol.TypeHoleCards:
			var p protocol.HoleCardsPayload
			if err := e.Decode(&p); err != nil {
				t.Fatalf("decode hole_cards: %v", err)
			}
			if len(p.Cards) != 2 {
				t.Fatalf("spectator hole_cards for seat %d has %d cards, want 2", p.Seat, len(p.Cards))
			}
			holeSeatsSeenBySpectator[p.Seat] = true
		}
	}
	if !sawReasoning {
		t.Fatal("spectator never received a reasoning event (expected at least one from the mock LLM seat)")
	}
	if len(holeSeatsSeenBySpectator) == 3 {
		sawAllHoleCardsSeats = true
	}
	if !sawAllHoleCardsSeats {
		t.Fatalf("spectator should see hole_cards for all 3 seats across 3 hands, saw: %v", holeSeatsSeenBySpectator)
	}

	// --- player:2 assertions ---
	for _, e := range playerAll {
		if e.Type == protocol.TypeReasoning {
			t.Fatalf("player:2 must NEVER receive a reasoning event, got %+v", e)
		}
		if e.Type == protocol.TypeHoleCards {
			var p protocol.HoleCardsPayload
			if err := e.Decode(&p); err != nil {
				t.Fatalf("decode hole_cards: %v", err)
			}
			if p.Seat != 2 {
				t.Fatalf("player:2 received hole_cards for seat %d, must only ever see its own", p.Seat)
			}
		}
		if e.Type == protocol.TypeSnapshot {
			var p protocol.SnapshotPayload
			if err := e.Decode(&p); err != nil {
				t.Fatalf("decode snapshot: %v", err)
			}
			for _, s := range p.Seats {
				if s.Seat != 2 && s.Hole != nil {
					t.Fatalf("player:2's snapshot leaks seat %d's hole cards: %+v", s.Seat, s)
				}
			}
		}
	}
}
