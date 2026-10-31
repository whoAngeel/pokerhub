package tui

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/whoAngeel/pokerhub/internal/protocol"
)

// TestModel_ReplaySample feeds recordings/sample.jsonl's raw envelope
// stream into Model.Update exactly as a live connection would (minus the
// bootstrap "snapshot" a real connection gets first -- this recording is
// the god-view stream alone, same as what `pokerd replay` plays into the
// Hub), then asserts the final View() shows the last hand's board and a
// winner/pot line. This is the one test docs/TUI_PLAN.md's T1 calls for.
func TestModel_ReplaySample(t *testing.T) {
	f, err := os.Open("../../recordings/sample.jsonl")
	if err != nil {
		t.Fatalf("open sample.jsonl: %v", err)
	}
	defer f.Close()

	var m tea.Model = NewModel(true, nil) // --ascii: deterministic, no glyph/emoji ambiguity in the assertion
	_ = m.Init()
	m, _ = m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	n := 0
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var env protocol.Envelope
		if err := json.Unmarshal([]byte(line), &env); err != nil {
			t.Fatalf("line %d: decode envelope: %v", n+1, err)
		}
		m, _ = m.Update(EnvMsg{Env: env})
		n++
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan sample.jsonl: %v", err)
	}
	if n == 0 {
		t.Fatal("sample.jsonl had no envelopes")
	}

	view := m.View()
	content := view.Content

	// The recording's last hand (hand_id 2) ends with street{river,
	// board:["6c","3d","7h","Jd","Th"]} and pot_awarded{winners:[1]}
	// (docs/PROTOCOL.md §4; see recordings/sample.jsonl's tail).
	for _, want := range []string{"6c", "3d", "7h", "Jd", "Th"} {
		if !strings.Contains(content, want) {
			t.Errorf("View() missing last board card %q\n--- content ---\n%s", want, content)
		}
	}
	if !strings.Contains(content, "bote") || !strings.Contains(content, "1080") {
		t.Errorf("View() missing a winner/pot line (want \"bote\" and pot amount 1080)\n--- content ---\n%s", content)
	}

	// docs/TUI_PLAN.md T2 "Names everywhere": the pot_awarded log line (and
	// every action line before it) must name the seat ("El Toro"), not
	// "seat 1"/"#1" -- the root-cause bug this phase fixed was
	// TableState.Apply's hand_start case never copying Seat.Name into its
	// seatState (internal/protocol/state.go).
	if !strings.Contains(content, "El Toro") {
		t.Errorf("View() log should use the winner's real name (\"El Toro\"), not a bare seat index\n--- content ---\n%s", content)
	}
	if strings.Contains(content, "seat 1 ") || strings.Contains(content, "#1 ") {
		t.Errorf("View() log still has a raw seat placeholder instead of a name\n--- content ---\n%s", content)
	}

	// docs/TUI_PLAN.md T2 "Side panel with tabs": every "reasoning" event
	// in the recording should have landed in the Razonamiento tab's
	// viewport, wrapped into hand/odds/reads/plan lines.
	tm, ok := m.(Model)
	if !ok {
		t.Fatalf("Update returned unexpected model type %T", m)
	}
	if len(tm.reasonBuf) == 0 {
		t.Fatal("reasonBuf is empty: no \"reasoning\" events from the recording reached the side panel")
	}
	reasonContent := tm.reasonVP.GetContent()
	if !strings.Contains(reasonContent, "plan:") {
		t.Errorf("Razonamiento tab content missing a \"plan:\" line\n--- content ---\n%s", reasonContent)
	}
}
