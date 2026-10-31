package replay

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/whoAngeel/pokerhub/internal/player"
	"github.com/whoAngeel/pokerhub/internal/protocol"
	"github.com/whoAngeel/pokerhub/internal/server"
	"github.com/whoAngeel/pokerhub/internal/table"
)

func testSeats() []protocol.Seat {
	return []protocol.Seat{
		{Seat: 0, Name: "A", Kind: "rulebot", Stack: 500, Status: "active"},
		{Seat: 1, Name: "B", Kind: "rulebot", Stack: 500, Status: "active"},
		{Seat: 2, Name: "C", Kind: "rulebot", Stack: 500, Status: "active"},
	}
}

// recordedLineCount reports how many JSON lines path contains and asserts
// each one parses as protocol.Envelope with a strictly increasing Seq --
// the same validation the task asks sample.jsonl itself to pass.
func validateRecording(t *testing.T, path string) int {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	var prevSeq int64
	n := 0
	for scanner.Scan() {
		var env protocol.Envelope
		if err := json.Unmarshal(scanner.Bytes(), &env); err != nil {
			t.Fatalf("line %d: not a valid protocol.Envelope: %v", n+1, err)
		}
		if n > 0 && env.Seq <= prevSeq {
			t.Fatalf("line %d: seq %d not strictly increasing after %d", n+1, env.Seq, prevSeq)
		}
		prevSeq = env.Seq
		n++
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan %s: %v", path, err)
	}
	return n
}

// TestRoundTrip_RecorderToReplay_SameProjectedSequence is the task's
// required round-trip test: record a real 3-hand session (live Hub +
// Recorder fed by the same table.MultiSink), then replay the recorded file
// into a brand-new Hub, and assert that for both spectator and player:1
// the (type, decoded payload) sequence Project() hands out is IDENTICAL --
// seq/ts necessarily differ (replay re-stamps with its own fresh counter),
// so those are deliberately excluded from the comparison.
func TestRoundTrip_RecorderToReplay_SameProjectedSequence(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")

	liveHub := server.NewHub(testSeats(), 2000, 0)
	rec, err := NewRecorder(path, 2000)
	if err != nil {
		t.Fatalf("NewRecorder: %v", err)
	}

	// Subscribe to the LIVE hub for both roles before any event fires, to
	// capture exactly what a connected client would have seen in real time.
	specSub, specInit := liveHub.Connect(server.Spectate(), nil, 4096)
	playerSub, playerInit := liveHub.Connect(server.PlayerSeat(1), nil, 4096)

	players := []player.Player{
		player.NewRuleBot("A"), player.NewRuleBot("B"), player.NewRuleBot("C"),
	}
	tcfg := table.Config{SmallBlind: 5, BigBlind: 10, BuyIn: 500, Seed: 99, DecisionTimeoutMS: 2000, EquityIters: 50}
	sess, err := table.NewSession(tcfg, players, table.MultiSink{liveHub, rec})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if err := sess.Run(context.Background(), 3); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := rec.Close(); err != nil {
		t.Fatalf("rec.Close: %v", err)
	}

	n := validateRecording(t, path)
	if n == 0 {
		t.Fatal("recording is empty")
	}

	liveSpec := append(specInit, drainChan(specSub)...)
	livePlayer := append(playerInit, drainChan(playerSub)...)
	if len(liveSpec) == 0 || len(livePlayer) == 0 {
		t.Fatal("captured zero live envelopes")
	}

	// Now replay the SAME file into a FRESH hub, subscribing the same two
	// roles before starting playback.
	replayHub := server.NewHub(testSeats(), 2000, 0)
	rSpecSub, rSpecInit := replayHub.Connect(server.Spectate(), nil, 4096)
	rPlayerSub, rPlayerInit := replayHub.Connect(server.PlayerSeat(1), nil, 4096)

	p := NewPlayer(path, 1000, false) // speed 1000x: the test shouldn't wait on real recorded gaps
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := p.Run(ctx, replayHub); err != nil {
		t.Fatalf("Player.Run: %v", err)
	}

	replaySpec := append(rSpecInit, drainChan(rSpecSub)...)
	replayPlayer := append(rPlayerInit, drainChan(rPlayerSub)...)

	assertSameProjectedSequence(t, "spectator", liveSpec, replaySpec)
	assertSameProjectedSequence(t, "player:1", livePlayer, replayPlayer)
}

func drainChan(sub *server.Subscription) []protocol.Envelope {
	var out []protocol.Envelope
	for {
		select {
		case e := <-sub.Envelopes():
			out = append(out, e)
		case <-time.After(200 * time.Millisecond):
			return out
		}
	}
}

func assertSameProjectedSequence(t *testing.T, label string, live, replayed []protocol.Envelope) {
	t.Helper()
	if len(live) != len(replayed) {
		t.Fatalf("%s: live had %d envelopes, replay had %d", label, len(live), len(replayed))
	}
	for i := range live {
		if live[i].Type != replayed[i].Type {
			t.Fatalf("%s: event %d type mismatch: live=%s replay=%s", label, i, live[i].Type, replayed[i].Type)
		}
		if string(live[i].Payload) != string(replayed[i].Payload) {
			t.Fatalf("%s: event %d (%s) payload mismatch:\n live:    %s\n replay:  %s",
				label, i, live[i].Type, live[i].Payload, replayed[i].Payload)
		}
		// hand_id must match too (just not seq/ts, which the replay hub
		// necessarily re-stamps with its own fresh counter).
		if (live[i].HandID == nil) != (replayed[i].HandID == nil) {
			t.Fatalf("%s: event %d hand_id nilness mismatch", label, i)
		}
		if live[i].HandID != nil && *live[i].HandID != *replayed[i].HandID {
			t.Fatalf("%s: event %d hand_id mismatch: live=%d replay=%d", label, i, *live[i].HandID, *replayed[i].HandID)
		}
	}
}
