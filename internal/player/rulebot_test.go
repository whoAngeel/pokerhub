package player

import (
	"context"
	"testing"
)

func TestRuleBot_NameAndKind(t *testing.T) {
	b := NewRuleBot("Calculín")
	if b.Name() != "Calculín" {
		t.Errorf("Name() = %q, want Calculín", b.Name())
	}
	if b.Kind() != "rulebot" {
		t.Errorf("Kind() = %q, want rulebot", b.Kind())
	}
}

func TestRuleBot_Decide_Policy(t *testing.T) {
	tests := []struct {
		name       string
		equity     float64
		potOdds    float64
		legal      []LegalAction
		wantType   string
		wantAmount int
	}{
		{
			name:   "strong equity raises to min when raise is legal",
			equity: 0.80, potOdds: 0.2,
			legal:    []LegalAction{{Type: "fold"}, {Type: "call", Amount: 40}, {Type: "raise", Min: 120, Max: 880}},
			wantType: "raise", wantAmount: 120,
		},
		{
			name:   "strong equity bets to min when only bet is legal",
			equity: 0.90, potOdds: 0.0,
			legal:    []LegalAction{{Type: "check"}, {Type: "bet", Min: 20, Max: 880}},
			wantType: "bet", wantAmount: 20,
		},
		{
			name:   "strong equity falls back to call when raise/bet aren't legal",
			equity: 0.70, potOdds: 0.3,
			legal:    []LegalAction{{Type: "fold"}, {Type: "call", Amount: 50}},
			wantType: "call", wantAmount: 50,
		},
		{
			name:   "equity above pot odds calls",
			equity: 0.40, potOdds: 0.25,
			legal:    []LegalAction{{Type: "fold"}, {Type: "call", Amount: 30}},
			wantType: "call", wantAmount: 30,
		},
		{
			name:   "equity above pot odds checks when free",
			equity: 0.30, potOdds: 0.10,
			legal:    []LegalAction{{Type: "check"}, {Type: "bet", Min: 10, Max: 500}},
			wantType: "check", wantAmount: 0,
		},
		{
			name:   "equity below pot odds checks if free",
			equity: 0.10, potOdds: 0.50,
			legal:    []LegalAction{{Type: "check"}, {Type: "bet", Min: 10, Max: 500}},
			wantType: "check", wantAmount: 0,
		},
		{
			name:   "equity below pot odds folds when not free",
			equity: 0.05, potOdds: 0.50,
			legal:    []LegalAction{{Type: "fold"}, {Type: "call", Amount: 100}, {Type: "raise", Min: 200, Max: 880}},
			wantType: "fold", wantAmount: 0,
		},
		{
			name:   "equity exactly at the 0.65 threshold counts as strong",
			equity: 0.65, potOdds: 0.1,
			legal:    []LegalAction{{Type: "fold"}, {Type: "raise", Min: 50, Max: 880}},
			wantType: "raise", wantAmount: 50,
		},
		{
			name:   "equity exactly equal to pot_odds counts as call-worthy",
			equity: 0.20, potOdds: 0.20,
			legal:    []LegalAction{{Type: "fold"}, {Type: "call", Amount: 20}},
			wantType: "call", wantAmount: 20,
		},
		{
			name:   "only fold legal: never raises/calls/checks into nothing",
			equity: 0.99, potOdds: 0.01,
			legal:    []LegalAction{{Type: "fold"}},
			wantType: "fold", wantAmount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := NewRuleBot("bot")
			v := DecisionView{
				Helpers:      Helpers{Equity: tt.equity, PotOdds: tt.potOdds},
				LegalActions: tt.legal,
			}
			d := b.Decide(context.Background(), v)
			if d.Action.Type != tt.wantType {
				t.Errorf("Action.Type = %q, want %q", d.Action.Type, tt.wantType)
			}
			if d.Action.Amount != tt.wantAmount {
				t.Errorf("Action.Amount = %d, want %d", d.Action.Amount, tt.wantAmount)
			}
			if d.Reasoning != nil {
				t.Errorf("Reasoning = %+v, want nil for RuleBot", d.Reasoning)
			}
			if d.Say != "" {
				t.Errorf("Say = %q, want empty for RuleBot", d.Say)
			}
		})
	}
}

func TestRuleBot_Decide_NoRandomnessAcrossRepeatedCalls(t *testing.T) {
	b := NewRuleBot("bot")
	v := DecisionView{
		Helpers:      Helpers{Equity: 0.72, PotOdds: 0.3},
		LegalActions: []LegalAction{{Type: "fold"}, {Type: "raise", Min: 100, Max: 880}},
	}
	first := b.Decide(context.Background(), v)
	for i := 0; i < 20; i++ {
		got := b.Decide(context.Background(), v)
		if got.Action != first.Action {
			t.Fatalf("call %d: Action = %+v, want deterministic %+v", i, got.Action, first.Action)
		}
	}
}

func TestRuleBot_Decide_IgnoresCanceledCtx(t *testing.T) {
	b := NewRuleBot("bot")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	v := DecisionView{
		Helpers:      Helpers{Equity: 0.1, PotOdds: 0.5},
		LegalActions: []LegalAction{{Type: "check"}},
	}
	d := b.Decide(ctx, v)
	if d.Action.Type != "check" {
		t.Errorf("Action.Type = %q, want check even with a canceled ctx (RuleBot is pure computation)", d.Action.Type)
	}
}
