package player

import "context"

// RuleBot is a deterministic, no-LLM Player: a simple tight-passive policy
// driven entirely by DecisionView.Helpers (equity vs pot_odds). No
// randomness, so it's reproducible and cheap to run as a baseline opponent.
//
// Policy (PLAN §2's "player/RuleBot (determinista)"):
//   - equity >= 0.65: raise to min (or bet min) if legal; otherwise call,
//     then check, then all_in, then fold, whichever is the strongest legal
//     option available.
//   - equity >= pot_odds: call if legal, else check, else all_in, else fold.
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

	var action Action
	switch {
	case equity >= 0.65:
		action = pickAggressive(legal)
	case equity >= potOdds:
		action = pickPassive(legal)
	default:
		action = pickFreeOrFold(legal)
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

// pickAggressive is the equity>=0.65 tier: raise/bet to the table minimum,
// falling back to progressively more passive legal options rather than ever
// folding a strong hand when aggression isn't available.
func pickAggressive(legal map[string]LegalAction) Action {
	if la, ok := legal["raise"]; ok {
		return Action{Type: "raise", Amount: la.Min}
	}
	if la, ok := legal["bet"]; ok {
		return Action{Type: "bet", Amount: la.Min}
	}
	return pickPassive(legal)
}

// pickPassive is the equity>=pot_odds tier: call, or check if there's
// nothing to call, or go all_in if that's the only way to continue, or fold.
func pickPassive(legal map[string]LegalAction) Action {
	if la, ok := legal["call"]; ok {
		return Action{Type: "call", Amount: la.Amount}
	}
	return pickFreeOrFold(legal)
}

// pickFreeOrFold is the bottom tier: check if it's free, otherwise go all_in
// only if that's literally the sole way to stay in the hand, otherwise fold.
func pickFreeOrFold(legal map[string]LegalAction) Action {
	if _, ok := legal["check"]; ok {
		return Action{Type: "check", Amount: 0}
	}
	if la, ok := legal["all_in"]; ok {
		if _, callOK := legal["call"]; !callOK {
			if _, foldOK := legal["fold"]; !foldOK {
				return Action{Type: "all_in", Amount: la.Amount}
			}
		}
	}
	return Action{Type: "fold", Amount: 0}
}
