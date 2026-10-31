package player

import "context"

// RuleBot is a deterministic, no-LLM Player: a simple tight-passive policy
// driven entirely by DecisionView.Helpers (equity vs pot_odds). No
// randomness, so it's reproducible and cheap to run as a baseline opponent.
//
// Policy:
//   - equity >= 0.65: raise to min (or bet min) if legal; if neither is
//     legal, fall back to the next tier instead of folding a strong hand.
//   - equity >= pot_odds: call if legal, else check if legal; if neither,
//     fall back to the bottom tier.
//   - otherwise: check if free, else fold.
type RuleBot struct {
	name string
}

// NewRuleBot builds a RuleBot with the given table-facing name.
func NewRuleBot(name string) *RuleBot {
	return &RuleBot{name: name}
}

func (b *RuleBot) Name() string { return b.name }
func (b *RuleBot) Kind() string { return "rulebot" }

// Decide implements Player. It ignores ctx: the policy is pure computation
// over v, so it never blocks.
func (b *RuleBot) Decide(_ context.Context, v DecisionView) Decision {
	legal := make(map[string]LegalAction, len(v.LegalActions))
	for _, la := range v.LegalActions {
		legal[la.Type] = la
	}

	equity := v.Helpers.Equity
	potOdds := v.Helpers.PotOdds

	action, ok := Action{}, false
	if equity >= 0.65 {
		action, ok = raiseOrBet(legal)
	}
	if !ok && equity >= potOdds {
		action, ok = callOrCheck(legal)
	}
	if !ok {
		action = checkOrFold(legal)
	}

	return Decision{
		Action: action,
		Say:    "",
		Meta: DecisionMeta{
			Provider: "rulebot",
			Model:    "rulebot",
		},
	}
}

func raiseOrBet(legal map[string]LegalAction) (Action, bool) {
	if la, ok := legal["raise"]; ok {
		return Action{Type: "raise", Amount: la.Min}, true
	}
	if la, ok := legal["bet"]; ok {
		return Action{Type: "bet", Amount: la.Min}, true
	}
	return Action{}, false
}

func callOrCheck(legal map[string]LegalAction) (Action, bool) {
	if la, ok := legal["call"]; ok {
		return Action{Type: "call", Amount: la.Amount}, true
	}
	if _, ok := legal["check"]; ok {
		return Action{Type: "check", Amount: 0}, true
	}
	return Action{}, false
}

func checkOrFold(legal map[string]LegalAction) Action {
	if _, ok := legal["check"]; ok {
		return Action{Type: "check", Amount: 0}
	}
	return Action{Type: "fold", Amount: 0}
}
