package table

import (
	"context"
	"testing"

	"github.com/whoAngeel/pokerhub/internal/llm"
	"github.com/whoAngeel/pokerhub/internal/memory"
	"github.com/whoAngeel/pokerhub/internal/player"
)

// alwaysCheckOrCallPlayer never folds or raises, so a heads-up hand between
// two of them is guaranteed to run every street with no extra betting and
// reach showdown deterministically -- exactly what this test needs to
// exercise the real EvHandEnd -> maybeUpdateMemory path.
type alwaysCheckOrCallPlayer struct{ name string }

func (p *alwaysCheckOrCallPlayer) Name() string { return p.name }
func (p *alwaysCheckOrCallPlayer) Kind() string { return "test" }
func (p *alwaysCheckOrCallPlayer) Decide(_ context.Context, v player.DecisionView) player.Decision {
	for _, la := range v.LegalActions {
		if la.Type == "check" {
			return player.Decision{Action: player.Action{Type: "check"}}
		}
	}
	for _, la := range v.LegalActions {
		if la.Type == "call" {
			return player.Decision{Action: player.Action{Type: "call", Amount: la.Amount}}
		}
	}
	return player.Decision{Action: player.Action{Type: "fold"}}
}

// TestSession_MemoryNotesArePrivatePerObserver plays one showdown hand
// between two seats, each with its own scripted memory.AgentInfo, then
// waits for the resulting asynchronous notes update (table/memory.go) and
// plays a second hand. It asserts the observing seat's next DecisionView
// carries the scripted note about its rival (player.Opponent.Notes), and
// that the OTHER seat -- which ran its own, differently-scripted update --
// never sees it: notes are private per observing agent (PLAN §7), never
// shared into another agent's prompt.
func TestSession_MemoryNotesArePrivatePerObserver(t *testing.T) {
	cfg := defaultConfig()
	cfg.BuyIn = 5000 // plenty relative to blinds: never forces an early all-in

	seat0 := newRecordingPlayer(&alwaysCheckOrCallPlayer{name: "Agente0"})
	seat1 := newRecordingPlayer(&alwaysCheckOrCallPlayer{name: "Agente1"})

	sink := &captureSink{}
	sess, err := NewSession(cfg, []player.Player{seat0, seat1}, sink)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}

	// Seat 0 "learns" a note about seat 1; seat 1's own update comes back
	// empty, so it learns nothing about seat 0 from this hand.
	seat0Provider := llm.NewMock(llm.MockResponse{Content: `{"notes":{"1":["Agente1 farolea en el river"]}}`})
	seat1Provider := llm.NewMock(llm.MockResponse{Content: `{"notes":{}}`})
	sess.SetMemoryAgents([]memory.AgentInfo{
		{Seat: 0, PersonaName: "Agente0", Provider: seat0Provider, Model: "test-model"},
		{Seat: 1, PersonaName: "Agente1", Provider: seat1Provider, Model: "test-model"},
	})

	ctx := context.Background()
	sess.playHand(ctx) // hand 1: both check/call every street -> showdown, spawns the two note updates
	sess.noteWG.Wait() // deterministically wait for both to land before hand 2's decisions are built
	sess.playHand(ctx) // hand 2: DecisionViews built here should reflect whatever landed

	var sawShowdown bool
	for _, e := range sink.all() {
		if e.Type == EvShowdown {
			sawShowdown = true
		}
	}
	if !sawShowdown {
		t.Fatalf("expected hand 1 to reach showdown, events: %+v", sink.all())
	}

	v0 := lastView(t, seat0)
	v1 := lastView(t, seat1)

	notesSeat0SeesOfSeat1 := opponentNotes(v0, 1)
	if len(notesSeat0SeesOfSeat1) != 1 || notesSeat0SeesOfSeat1[0] != "Agente1 farolea en el river" {
		t.Fatalf("seat 0 (observer): expected the scripted note about seat 1, got %v", notesSeat0SeesOfSeat1)
	}

	notesSeat1SeesOfSeat0 := opponentNotes(v1, 0)
	if len(notesSeat1SeesOfSeat0) != 0 {
		t.Fatalf("seat 1 must never see seat 0's private note, got %v", notesSeat1SeesOfSeat0)
	}
}

func lastView(t *testing.T, rp *recordingPlayer) player.DecisionView {
	t.Helper()
	views := rp.Views()
	if len(views) == 0 {
		t.Fatalf("no DecisionViews recorded")
	}
	return views[len(views)-1]
}

func opponentNotes(v player.DecisionView, seat int) []string {
	for _, opp := range v.Opponents {
		if opp.Seat == seat {
			return opp.Notes
		}
	}
	return nil
}
