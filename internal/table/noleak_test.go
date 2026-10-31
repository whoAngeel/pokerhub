package table

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"testing"

	"github.com/whoAngeel/pokerhub/internal/player"
)

// cardToken matches card-shaped substrings ("Ah", "Td", "9c", ...) anywhere
// in a marshaled DecisionView, the same detector shape PLAN §4.4's
// TestNoLeak describes.
var cardToken = regexp.MustCompile(`\b[2-9TJQKA][shdc]\b`)

// TestNoOpponentHoleCardLeak runs many hands and, for every DecisionView any
// seat was ever shown, asserts every card token found in its JSON encoding
// belongs to that seat's own hole cards or the board -- i.e. nothing else
// (another seat's hole cards) ever appears, exactly as PLAN §4.4 requires.
func TestNoOpponentHoleCardLeak(t *testing.T) {
	cfg := defaultConfig()
	n := 6
	players := make([]player.Player, n)
	recorders := make([]*recordingPlayer, n)
	for i := 0; i < n; i++ {
		var base player.Player
		if i%2 == 0 {
			base = player.NewRuleBot(fmt.Sprintf("Bot%d", i))
		} else {
			base = newMockAgent(fmt.Sprintf("Agent%d", i))
		}
		rp := newRecordingPlayer(base)
		recorders[i] = rp
		players[i] = rp
	}

	sink := &captureSink{}
	sess, err := NewSession(cfg, players, sink)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := sess.Run(context.Background(), 200); err != nil {
		t.Fatalf("Run: %v", err)
	}

	checked := 0
	for seat, rp := range recorders {
		for _, v := range rp.Views() {
			checked++
			assertNoLeak(t, seat, v)
		}
	}
	if checked == 0 {
		t.Fatalf("no DecisionViews were recorded")
	}
}

func assertNoLeak(t *testing.T, seat int, v player.DecisionView) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("seat %d: marshal DecisionView: %v", seat, err)
	}
	allowed := map[string]bool{}
	for _, c := range v.You.Hole {
		allowed[c] = true
	}
	for _, c := range v.Board {
		allowed[c] = true
	}
	for _, tok := range cardToken.FindAllString(string(data), -1) {
		if !allowed[tok] {
			t.Fatalf("seat %d: leaked card token %q (hole=%v board=%v) in view: %s", seat, tok, v.You.Hole, v.Board, data)
		}
	}
	if v.You.Seat != seat {
		t.Fatalf("view built for seat %d claims You.Seat=%d", seat, v.You.Seat)
	}
	for _, opp := range v.Opponents {
		if opp.Seat == seat {
			t.Fatalf("seat %d: own seat appears in Opponents list", seat)
		}
	}
}
