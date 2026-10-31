package agent

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode"

	"github.com/atzomx/pokerllm/internal/player"
)

// rawDecision is the wire shape of DecisionSchema.
type rawDecision struct {
	Reasoning rawReasoning `json:"reasoning"`
	Action    string       `json:"action"`
	Amount    int          `json:"amount"`
	Say       string       `json:"say"`
}

type rawReasoning struct {
	Hand  string `json:"hand"`
	Odds  string `json:"odds"`
	Reads string `json:"reads"`
	Plan  string `json:"plan"`
}

// validActions is DecisionSchema's action enum.
var validActions = map[string]bool{
	"fold": true, "check": true, "call": true,
	"bet": true, "raise": true, "all_in": true,
}

// ParseAndNormalize turns a raw model response into a player.Decision,
// applying PLAN §4.3's deterministic normalization (no LLM involved). Each
// correction is appended as a short code to the returned Decision's
// Meta.Warnings.
//
// A non-nil error means the output could not be salvaged (invalid JSON, an
// action outside the schema's enum, or an action that stays illegal even
// after every normalization swap) and the caller (agent.Decide) should
// retry once before falling back.
func ParseAndNormalize(content string, v player.DecisionView) (player.Decision, error) {
	var raw rawDecision
	if err := json.Unmarshal([]byte(content), &raw); err != nil {
		return player.Decision{}, fmt.Errorf("agent: invalid JSON output: %w", err)
	}
	if !validActions[raw.Action] {
		return player.Decision{}, fmt.Errorf("agent: unknown action %q", raw.Action)
	}

	actionType, amount, warnings, ok := normalizeAction(raw.Action, raw.Amount, v.LegalActions)
	if !ok {
		return player.Decision{}, fmt.Errorf("agent: action %q is not legal (legal: %s)", raw.Action, legalActionsSpanish(v.LegalActions))
	}

	reasoning := &player.Reasoning{
		Hand:  raw.Reasoning.Hand,
		Odds:  raw.Reasoning.Odds,
		Reads: raw.Reasoning.Reads,
		Plan:  raw.Reasoning.Plan,
	}

	say, sayWarnings := normalizeSay(raw.Say, reasoning)
	warnings = append(warnings, sayWarnings...)

	if bad := detectHallucinatedCards(reasoning, v); bad != "" {
		warnings = append(warnings, "hallucinated_cards:"+bad)
	}

	return player.Decision{
		Action:    player.Action{Type: actionType, Amount: amount},
		Reasoning: reasoning,
		Say:       say,
		Meta:      player.DecisionMeta{Warnings: warnings},
	}, nil
}

// legalByType indexes legal actions by type. In practice there's at most one
// LegalAction per type.
func legalByType(actions []player.LegalAction) map[string]player.LegalAction {
	m := make(map[string]player.LegalAction, len(actions))
	for _, a := range actions {
		m[a.Type] = a
	}
	return m
}

// normalizeAction applies PLAN §4.3's deterministic corrections:
//   - call with to_call==0 (i.e. "call" isn't legal but "check" is) -> check
//   - check with to_call>0 (i.e. "check" isn't legal but "call" is) -> call
//   - bet <-> raise, swapped to whichever of the two is actually legal
//   - a short stack where only all_in covers what the model asked for
//   - amount clamped into [min,max] for bet/raise
//   - amount reaching the all-in ceiling -> promoted to all_in
//   - amount for call/check/fold forced to the value the legal action dictates
//
// ok is false when action can't be made legal by any of the above; the
// caller then treats it like a parse failure (retry, then fallback).
func normalizeAction(action string, amount int, legal []player.LegalAction) (string, int, []string, bool) {
	byType := legalByType(legal)
	var warnings []string

	if _, ok := byType[action]; !ok {
		swapped, warn := swapIllegalAction(action, byType)
		if swapped == "" {
			return "", 0, warnings, false
		}
		warnings = append(warnings, warn)
		action = swapped
	}

	la := byType[action]

	switch action {
	case "fold", "check":
		if amount != 0 {
			warnings = append(warnings, "forced_amount:"+action)
		}
		return action, 0, warnings, true

	case "call", "all_in":
		if amount != la.Amount {
			warnings = append(warnings, "forced_amount:"+action)
		}
		return action, la.Amount, warnings, true

	case "bet", "raise":
		amt := amount
		if amt < la.Min {
			amt = la.Min
			warnings = append(warnings, "clamped_amount")
		} else if amt > la.Max {
			amt = la.Max
			warnings = append(warnings, "clamped_amount")
		}
		// Reaching the bet/raise ceiling (after clamping) means going
		// all-in (PLAN §4.3: "amount >= stack -> all_in"). When the engine
		// also lists a separate "all_in" legal action, that one carries the
		// authoritative chip amount (la.Max is only this bet/raise's own
		// cap, which isn't necessarily the real all-in total) and wins over
		// the clamped amount.
		allin, hasAllin := byType["all_in"]
		reachedCeiling := amt >= la.Max || (hasAllin && amt >= allin.Amount)
		if reachedCeiling {
			warnings = append(warnings, "normalized:"+action+"->all_in")
			if hasAllin {
				return "all_in", allin.Amount, warnings, true
			}
			return "all_in", amt, warnings, true
		}
		return action, amt, warnings, true

	default:
		// Unreachable: action is one of the six schema values and every case
		// above is covered.
		return "", 0, warnings, false
	}
}

// swapIllegalAction tries the single deterministic swap PLAN §4.3 describes
// for an action that isn't currently legal. It returns "" when no swap
// applies, leaving the caller to fail (retry/fallback).
func swapIllegalAction(action string, byType map[string]player.LegalAction) (swapped string, warning string) {
	try := func(candidates ...string) (string, string) {
		for _, c := range candidates {
			if _, ok := byType[c]; ok {
				return c, "normalized:" + action + "->" + c
			}
		}
		return "", ""
	}

	switch action {
	case "call":
		return try("check", "all_in")
	case "check":
		return try("call")
	case "bet":
		return try("raise", "all_in")
	case "raise":
		return try("bet", "all_in")
	case "all_in":
		return try("call", "bet", "raise")
	case "fold":
		return "", "" // fold is essentially always legal; nothing to swap to
	default:
		return "", ""
	}
}

// legalActionsSpanish renders legal actions for the retry prompt (PLAN
// §4.3 point 4: "Legales: …").
func legalActionsSpanish(actions []player.LegalAction) string {
	if len(actions) == 0 {
		return "(ninguna)"
	}
	parts := make([]string, 0, len(actions))
	for _, la := range actions {
		switch la.Type {
		case "fold":
			parts = append(parts, "fold")
		case "check":
			parts = append(parts, "check")
		case "call":
			parts = append(parts, fmt.Sprintf("call %d", la.Amount))
		case "bet":
			parts = append(parts, fmt.Sprintf("bet entre %d y %d", la.Min, la.Max))
		case "raise":
			parts = append(parts, fmt.Sprintf("raise entre %d y %d", la.Min, la.Max))
		case "all_in":
			parts = append(parts, fmt.Sprintf("all_in %d", la.Amount))
		default:
			parts = append(parts, la.Type)
		}
	}
	return strings.Join(parts, ", ")
}

// normalizeSay trims and caps say, and blanks it with a warning when it
// substantially echoes any reasoning field (a cheap way of catching a model
// that just copy-pastes its private chain-of-thought into the public chat).
func normalizeSay(say string, r *player.Reasoning) (string, []string) {
	var warnings []string

	say = strings.TrimSpace(say)
	if runes := []rune(say); len(runes) > sayCharLimit {
		say = string(runes[:sayCharLimit])
		warnings = append(warnings, "say_truncated")
	}

	if say != "" && echoesReasoning(say, r) {
		warnings = append(warnings, "say_echoes_reasoning")
		say = ""
	}

	return say, warnings
}

// echoesReasoning reports whether say "substantially equals" one of r's
// fields: same normalized text, or a long-enough normalized say that's
// contained in a normalized field.
func echoesReasoning(say string, r *player.Reasoning) bool {
	ns := normalizeForCompare(say)
	if ns == "" {
		return false
	}
	for _, f := range []string{r.Hand, r.Odds, r.Reads, r.Plan} {
		nf := normalizeForCompare(f)
		if nf == "" {
			continue
		}
		if ns == nf {
			return true
		}
		if len(ns) >= 15 && strings.Contains(nf, ns) {
			return true
		}
	}
	return false
}

// normalizeForCompare lowercases and strips everything but letters/digits,
// so punctuation/spacing differences don't defeat the echo check.
func normalizeForCompare(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// tenRe maps the occasional "10h"-style token (not a valid 2-char card code)
// onto the regular "Th" form before the card regex runs.
var tenRe = regexp.MustCompile(`\b10([shdc])\b`)

// cardTokenRe matches a 2-char poker card code: rank (2-9TJQKA) + suit
// (shdc), per PLAN §4.3's hallucination detector.
var cardTokenRe = regexp.MustCompile(`\b([2-9TJQKA][shdc])\b`)

// detectHallucinatedCards scans every reasoning field for card-shaped tokens
// and returns a sorted, deduplicated, comma-joined list of the ones that
// aren't in you.hole ∪ board — i.e. cards the model made up. Returns "" when
// there's nothing to report. This never blocks the action; it's advisory.
func detectHallucinatedCards(r *player.Reasoning, v player.DecisionView) string {
	if r == nil {
		return ""
	}
	known := make(map[string]bool, len(v.You.Hole)+len(v.Board))
	for _, c := range v.You.Hole {
		known[c] = true
	}
	for _, c := range v.Board {
		known[c] = true
	}

	text := strings.Join([]string{r.Hand, r.Odds, r.Reads, r.Plan}, " ")
	text = tenRe.ReplaceAllString(text, "T$1")

	seen := map[string]bool{}
	var bad []string
	for _, m := range cardTokenRe.FindAllString(text, -1) {
		if known[m] || seen[m] {
			continue
		}
		seen[m] = true
		bad = append(bad, m)
	}
	if len(bad) == 0 {
		return ""
	}
	sort.Strings(bad)
	return strings.Join(bad, ",")
}
