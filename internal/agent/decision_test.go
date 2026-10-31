package agent

import (
	"strings"
	"testing"

	"github.com/atzomx/pokerllm/internal/player"
)

func viewWithLegal(legal ...player.LegalAction) player.DecisionView {
	return player.DecisionView{
		You:          player.Me{Hole: []string{"Ah", "Kd"}},
		Board:        []string{"Ks", "7h", "2c"},
		LegalActions: legal,
	}
}

func rawJSON(t *testing.T, action string, amount int, say string) string {
	t.Helper()
	return `{"reasoning":{"hand":"h","odds":"o","reads":"r","plan":"p"},"action":"` +
		action + `","amount":` + itoa(amount) + `,"say":"` + say + `"}`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestParseAndNormalize_BadJSON(t *testing.T) {
	v := viewWithLegal(player.LegalAction{Type: "check"})
	_, err := ParseAndNormalize(`not json`, v)
	if err == nil {
		t.Fatal("want error for invalid JSON, got nil")
	}
}

func TestParseAndNormalize_UnknownAction(t *testing.T) {
	v := viewWithLegal(player.LegalAction{Type: "check"})
	_, err := ParseAndNormalize(rawJSON(t, "smash", 0, ""), v)
	if err == nil {
		t.Fatal("want error for an action outside the schema enum, got nil")
	}
}

func TestParseAndNormalize_IllegalWithNoSwap(t *testing.T) {
	// Only "fold" is legal; the model picks "bet", which has nothing to
	// swap to (no "raise", no "all_in").
	v := viewWithLegal(player.LegalAction{Type: "fold"})
	_, err := ParseAndNormalize(rawJSON(t, "bet", 50, ""), v)
	if err == nil {
		t.Fatal("want error: bet isn't legal and there's nothing to swap to")
	}
}

// TestNormalizeAction_Table covers every deterministic correction from
// PLAN §4.3 / the task spec, each as a case over normalizeAction directly so
// warnings and the resulting Action can be asserted precisely.
func TestNormalizeAction_Table(t *testing.T) {
	tests := []struct {
		name       string
		action     string
		amount     int
		legal      []player.LegalAction
		wantAction string
		wantAmount int
		wantWarn   string // substring expected somewhere in warnings; "" = none required
		wantOK     bool
	}{
		{
			name:   "call with to_call==0 normalizes to check",
			action: "call", amount: 0,
			legal:      []player.LegalAction{{Type: "check"}},
			wantAction: "check", wantAmount: 0,
			wantWarn: "normalized:call->check", wantOK: true,
		},
		{
			name:   "check with to_call>0 normalizes to call",
			action: "check", amount: 0,
			legal:      []player.LegalAction{{Type: "call", Amount: 60}},
			wantAction: "call", wantAmount: 60,
			wantWarn: "normalized:check->call", wantOK: true,
		},
		{
			name:   "bet swaps to raise when raise is legal",
			action: "bet", amount: 200,
			legal:      []player.LegalAction{{Type: "raise", Min: 120, Max: 880}},
			wantAction: "raise", wantAmount: 200,
			wantWarn: "normalized:bet->raise", wantOK: true,
		},
		{
			name:   "raise swaps to bet when bet is legal",
			action: "raise", amount: 80,
			legal:      []player.LegalAction{{Type: "bet", Min: 20, Max: 880}},
			wantAction: "bet", wantAmount: 80,
			wantWarn: "normalized:raise->bet", wantOK: true,
		},
		{
			name:   "raise amount below min is clamped up",
			action: "raise", amount: 10,
			legal:      []player.LegalAction{{Type: "raise", Min: 120, Max: 880}},
			wantAction: "raise", wantAmount: 120,
			wantWarn: "clamped_amount", wantOK: true,
		},
		{
			name:   "bet amount above max is clamped down",
			action: "bet", amount: 99999,
			legal:      []player.LegalAction{{Type: "bet", Min: 20, Max: 500}, {Type: "all_in", Amount: 880}},
			wantAction: "all_in", wantAmount: 880,
			wantWarn: "clamped_amount", wantOK: true,
		},
		{
			name:   "raise amount reaching the all-in ceiling promotes to all_in",
			action: "raise", amount: 880,
			legal:      []player.LegalAction{{Type: "raise", Min: 120, Max: 880}, {Type: "all_in", Amount: 880}},
			wantAction: "all_in", wantAmount: 880,
			wantWarn: "normalized:raise->all_in", wantOK: true,
		},
		{
			name:   "raise reaching max with no separate all_in entry still promotes",
			action: "raise", amount: 880,
			legal:      []player.LegalAction{{Type: "raise", Min: 120, Max: 880}},
			wantAction: "all_in", wantAmount: 880,
			wantWarn: "normalized:raise->all_in", wantOK: true,
		},
		{
			name:   "call amount forced from legal action",
			action: "call", amount: 1,
			legal:      []player.LegalAction{{Type: "call", Amount: 60}},
			wantAction: "call", wantAmount: 60,
			wantWarn: "forced_amount:call", wantOK: true,
		},
		{
			name:   "check amount forced to zero",
			action: "check", amount: 50,
			legal:      []player.LegalAction{{Type: "check"}},
			wantAction: "check", wantAmount: 0,
			wantWarn: "forced_amount:check", wantOK: true,
		},
		{
			name:   "fold amount forced to zero",
			action: "fold", amount: 50,
			legal:      []player.LegalAction{{Type: "fold"}},
			wantAction: "fold", wantAmount: 0,
			wantWarn: "forced_amount:fold", wantOK: true,
		},
		{
			name:   "already-legal action passes through with no warnings",
			action: "call", amount: 60,
			legal:      []player.LegalAction{{Type: "call", Amount: 60}},
			wantAction: "call", wantAmount: 60,
			wantWarn: "", wantOK: true,
		},
		{
			name:   "call falls back to all_in when only all_in is legal",
			action: "call", amount: 1,
			legal:      []player.LegalAction{{Type: "all_in", Amount: 40}},
			wantAction: "all_in", wantAmount: 40,
			wantWarn: "normalized:call->all_in", wantOK: true,
		},
		{
			name:   "fold with nothing to swap to fails",
			action: "fold", amount: 0,
			legal:  []player.LegalAction{{Type: "check"}},
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			action, amount, warnings, ok := normalizeAction(tt.action, tt.amount, tt.legal)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (action=%q amount=%d warnings=%v)", ok, tt.wantOK, action, amount, warnings)
			}
			if !tt.wantOK {
				return
			}
			if action != tt.wantAction {
				t.Errorf("action = %q, want %q", action, tt.wantAction)
			}
			if amount != tt.wantAmount {
				t.Errorf("amount = %d, want %d", amount, tt.wantAmount)
			}
			if tt.wantWarn != "" && !containsString(warnings, tt.wantWarn) {
				t.Errorf("warnings = %v, want one containing %q", warnings, tt.wantWarn)
			}
		})
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func TestParseAndNormalize_FullDecisionWithWarnings(t *testing.T) {
	v := viewWithLegal(player.LegalAction{Type: "check"})
	content := rawJSON(t, "call", 0, "")
	d, err := ParseAndNormalize(content, v)
	if err != nil {
		t.Fatalf("ParseAndNormalize error = %v", err)
	}
	if d.Action.Type != "check" {
		t.Errorf("Action.Type = %q, want check", d.Action.Type)
	}
	if !containsString(d.Meta.Warnings, "normalized:call->check") {
		t.Errorf("Warnings = %v, want normalized:call->check", d.Meta.Warnings)
	}
	if d.Reasoning == nil {
		t.Fatal("Reasoning should never be nil for a parsed LLM decision")
	}
}

func TestParseAndNormalize_SayTruncation(t *testing.T) {
	v := viewWithLegal(player.LegalAction{Type: "check"})
	longSay := strings.Repeat("a", 200)
	content := `{"reasoning":{"hand":"h","odds":"o","reads":"r","plan":"p"},"action":"check","amount":0,"say":"` + longSay + `"}`

	d, err := ParseAndNormalize(content, v)
	if err != nil {
		t.Fatalf("ParseAndNormalize error = %v", err)
	}
	if n := len([]rune(d.Say)); n != 120 {
		t.Errorf("len(Say) = %d, want 120", n)
	}
	if !containsString(d.Meta.Warnings, "say_truncated") {
		t.Errorf("Warnings = %v, want say_truncated", d.Meta.Warnings)
	}
}

func TestParseAndNormalize_SayEchoesReasoningIsBlanked(t *testing.T) {
	v := viewWithLegal(player.LegalAction{Type: "check"})
	content := `{"reasoning":{"hand":"Tengo pareja de reyes con buen kicker","odds":"o","reads":"r","plan":"p"},` +
		`"action":"check","amount":0,"say":"Tengo pareja de reyes con buen kicker"}`

	d, err := ParseAndNormalize(content, v)
	if err != nil {
		t.Fatalf("ParseAndNormalize error = %v", err)
	}
	if d.Say != "" {
		t.Errorf("Say = %q, want blanked (echoes reasoning.hand)", d.Say)
	}
	if !containsString(d.Meta.Warnings, "say_echoes_reasoning") {
		t.Errorf("Warnings = %v, want say_echoes_reasoning", d.Meta.Warnings)
	}
}

func TestParseAndNormalize_SayNotEchoingIsKept(t *testing.T) {
	v := viewWithLegal(player.LegalAction{Type: "check"})
	content := `{"reasoning":{"hand":"Tengo pareja de reyes","odds":"o","reads":"r","plan":"p"},` +
		`"action":"check","amount":0,"say":"¡Vaya mano!"}`

	d, err := ParseAndNormalize(content, v)
	if err != nil {
		t.Fatalf("ParseAndNormalize error = %v", err)
	}
	if d.Say != "¡Vaya mano!" {
		t.Errorf("Say = %q, want it kept unchanged", d.Say)
	}
	if containsString(d.Meta.Warnings, "say_echoes_reasoning") {
		t.Errorf("Warnings = %v, didn't expect say_echoes_reasoning", d.Meta.Warnings)
	}
}

func TestParseAndNormalize_HallucinatedCards(t *testing.T) {
	v := player.DecisionView{
		You:          player.Me{Hole: []string{"Ah", "Kd"}},
		Board:        []string{"Ks", "7h", "2c"},
		LegalActions: []player.LegalAction{{Type: "check"}},
	}
	content := `{"reasoning":{"hand":"Tengo Ah y Kd, pero también vi un 2s y un Jh en algún lado",` +
		`"odds":"con el 10h estaría mejor","reads":"r","plan":"p"},"action":"check","amount":0,"say":""}`

	d, err := ParseAndNormalize(content, v)
	if err != nil {
		t.Fatalf("ParseAndNormalize error = %v", err)
	}
	want := "hallucinated_cards:2s,Jh,Th"
	if !containsString(d.Meta.Warnings, want) {
		t.Errorf("Warnings = %v, want one equal to %q", d.Meta.Warnings, want)
	}
}

func TestParseAndNormalize_NoHallucinationForKnownCards(t *testing.T) {
	v := player.DecisionView{
		You:          player.Me{Hole: []string{"Ah", "Kd"}},
		Board:        []string{"Ks", "7h", "2c"},
		LegalActions: []player.LegalAction{{Type: "check"}},
	}
	content := `{"reasoning":{"hand":"Tengo Ah y Kd sobre un board con Ks, 7h y 2c",` +
		`"odds":"o","reads":"r","plan":"p"},"action":"check","amount":0,"say":""}`

	d, err := ParseAndNormalize(content, v)
	if err != nil {
		t.Fatalf("ParseAndNormalize error = %v", err)
	}
	for _, w := range d.Meta.Warnings {
		if strings.HasPrefix(w, "hallucinated_cards:") {
			t.Errorf("Warnings = %v, didn't expect a hallucinated_cards warning", d.Meta.Warnings)
		}
	}
}

func TestLegalActionsSpanish(t *testing.T) {
	legal := []player.LegalAction{
		{Type: "fold"},
		{Type: "call", Amount: 60},
		{Type: "raise", Min: 120, Max: 880},
	}
	out := legalActionsSpanish(legal)
	for _, want := range []string{"fold", "call 60", "raise entre 120 y 880"} {
		if !strings.Contains(out, want) {
			t.Errorf("legalActionsSpanish() = %q, want it to contain %q", out, want)
		}
	}
}
