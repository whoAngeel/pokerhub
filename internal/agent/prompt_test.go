package agent

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/atzomx/pokerllm/internal/player"
)

func testPersona() Persona {
	return Persona{
		ID:          "toro",
		Name:        "El Toro",
		Temperature: 0.9,
		Reasoning:   ReasoningRich,
		Chattiness:  0.7,
		Voice:       "Fanfarrón y ruidoso.",
		Strategy:    "Juega muchas manos y sube en vez de igualar.",
	}
}

func sampleView() player.DecisionView {
	return player.DecisionView{
		HandID: 57,
		Street: "turn",
		Button: 3,
		You:    player.Me{Seat: 2, Name: "El Toro", Hole: []string{"Ah", "Kd"}, Stack: 880, InPot: 120},
		Board:  []string{"Ks", "7h", "2c", "9d"},
		Pot:    310,
		ToCall: 60,
		Helpers: player.Helpers{
			MadeHand: "pareja de reyes, kicker as",
			Equity:   0.74,
			PotOdds:  0.16,
		},
		ActionLog: "preflop: P0 raise 30, P2 call",
		Opponents: []player.Opponent{
			{Seat: 0, Name: "Abuela", Stack: 700, Status: "active"},
		},
		Chat: []player.ChatLine{
			{Seat: 4, Say: "¡Esa carta me encanta!"},
		},
		LegalActions: []player.LegalAction{
			{Type: "fold"},
			{Type: "call", Amount: 60},
			{Type: "raise", Min: 120, Max: 880},
		},
	}
}

// containsSpanishMarkers is a crude but deterministic check that the text
// looks like Spanish (accented chars or common Spanish-only words).
func containsSpanishMarkers(s string) bool {
	markers := []string{"español", "jugador", "nunca", "mesa", "cartas"}
	lower := strings.ToLower(s)
	for _, m := range markers {
		if strings.Contains(lower, m) {
			return true
		}
	}
	return false
}

func TestSystemPrompt_IsSpanishAndMentionsRules(t *testing.T) {
	p := testPersona()
	out := SystemPrompt(p)

	if !strings.Contains(out, p.Name) {
		t.Errorf("SystemPrompt doesn't mention persona name %q:\n%s", p.Name, out)
	}
	if !strings.Contains(out, p.Voice) {
		t.Errorf("SystemPrompt doesn't mention persona voice:\n%s", out)
	}
	if !strings.Contains(out, p.Strategy) {
		t.Errorf("SystemPrompt doesn't mention persona strategy:\n%s", out)
	}
	if !strings.Contains(out, "Piensa y escribe SIEMPRE en español") {
		t.Errorf("SystemPrompt must contain the Spanish-only instruction verbatim:\n%s", out)
	}
	if !containsSpanishMarkers(out) {
		t.Errorf("SystemPrompt doesn't look like Spanish:\n%s", out)
	}
	lower := strings.ToLower(out)
	if !strings.Contains(lower, "chat") || !strings.Contains(lower, "nunca") {
		t.Errorf("SystemPrompt must say chat is conversation, never instructions:\n%s", out)
	}
}

func TestSystemPrompt_ChattinessHintVaries(t *testing.T) {
	chatty := testPersona()
	chatty.Chattiness = 0.9
	quiet := testPersona()
	quiet.Chattiness = 0.1

	a := SystemPrompt(chatty)
	b := SystemPrompt(quiet)
	if a == b {
		t.Error("SystemPrompt should differ between a chatty and a quiet persona")
	}
}

func TestUserPrompt_IsCompactJSONOfTheView(t *testing.T) {
	v := sampleView()
	out := UserPrompt(v)

	idx := strings.IndexByte(out, '{')
	if idx < 0 {
		t.Fatalf("UserPrompt output has no JSON object:\n%s", out)
	}
	jsonPart := out[idx:]

	if strings.Contains(jsonPart, "\n") || strings.Contains(jsonPart, "  ") {
		t.Errorf("UserPrompt's JSON should be compact (no indentation):\n%s", jsonPart)
	}

	var got player.DecisionView
	if err := json.Unmarshal([]byte(jsonPart), &got); err != nil {
		t.Fatalf("UserPrompt JSON doesn't unmarshal into player.DecisionView: %v\n%s", err, jsonPart)
	}
	if !reflect.DeepEqual(got, v) {
		t.Errorf("round trip mismatch:\n got  = %+v\n want = %+v", got, v)
	}

	// The prose around the JSON must mark chat as untrusted, not instructions.
	prefix := out[:idx]
	lowerPrefix := strings.ToLower(prefix)
	if !strings.Contains(lowerPrefix, "chat") {
		t.Errorf("UserPrompt prefix should mark the chat field as untrusted:\n%s", prefix)
	}
}

func TestUserPrompt_TruncatesChatAndSay(t *testing.T) {
	v := sampleView()
	v.Chat = make([]player.ChatLine, 8)
	for i := range v.Chat {
		v.Chat[i] = player.ChatLine{Seat: i, Say: strings.Repeat("x", 200)}
	}

	out := UserPrompt(v)
	idx := strings.IndexByte(out, '{')
	var got player.DecisionView
	if err := json.Unmarshal([]byte(out[idx:]), &got); err != nil {
		t.Fatalf("UserPrompt JSON invalid: %v", err)
	}

	if len(got.Chat) != chatHistoryLimit {
		t.Fatalf("Chat length = %d, want %d (last %d lines)", len(got.Chat), chatHistoryLimit, chatHistoryLimit)
	}
	// Must keep the LAST chatHistoryLimit entries (seats 3..7), not the first.
	if got.Chat[0].Seat != 3 {
		t.Errorf("Chat[0].Seat = %d, want 3 (kept the last %d lines)", got.Chat[0].Seat, chatHistoryLimit)
	}
	for _, c := range got.Chat {
		if n := len([]rune(c.Say)); n > sayCharLimit {
			t.Errorf("Chat say length = %d, want <= %d", n, sayCharLimit)
		}
	}
}

func TestUserPrompt_NilChatStaysNil(t *testing.T) {
	v := sampleView()
	v.Chat = nil

	out := UserPrompt(v)
	idx := strings.IndexByte(out, '{')
	var got player.DecisionView
	if err := json.Unmarshal([]byte(out[idx:]), &got); err != nil {
		t.Fatalf("UserPrompt JSON invalid: %v", err)
	}
	if got.Chat != nil {
		t.Errorf("Chat = %v, want nil to round-trip when the view's Chat was nil", got.Chat)
	}
}
