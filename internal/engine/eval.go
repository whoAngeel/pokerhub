package engine

import "github.com/chehsunliu/poker"

// Evaluator scores a 5-, 6- or 7-card hand. Lower is better (1 = best
// possible royal flush distinct value). It is the only place
// github.com/chehsunliu/poker is referenced, so the rest of the package
// stays independent of that library's types.
type Evaluator interface {
	// Evaluate scores cards (len must be 5, 6 or 7).
	Evaluate(cards []Card) int32
	// Class returns the evaluator-native hand class for a code:
	// 1=straight flush, 2=four of a kind, 3=full house, 4=flush,
	// 5=straight, 6=three of a kind, 7=two pair, 8=pair, 9=high card.
	Class(code int32) int32
}

type pokerEvaluator struct{}

func (pokerEvaluator) Evaluate(cards []Card) int32 {
	pc := make([]poker.Card, len(cards))
	for i, c := range cards {
		pc[i] = poker.NewCard(string(c))
	}
	return poker.Evaluate(pc)
}

func (pokerEvaluator) Class(code int32) int32 {
	return poker.RankClass(code)
}

// defaultEvaluator is the Evaluator used everywhere in the package.
var defaultEvaluator Evaluator = pokerEvaluator{}

// handClassNameEs maps an evaluator class (see Evaluator.Class) to a
// short Spanish category name. Class 8 (Pair) is special-cased by
// callers that want to name the paired rank ("pareja de reyes").
var handClassNameEs = map[int32]string{
	1: "escalera de color",
	2: "póker",
	3: "full",
	4: "color",
	5: "escalera",
	6: "trío",
	7: "doble pareja",
	8: "pareja",
	9: "carta alta",
}

// evaluate7 scores hole+board together. At showdown and in Equity the
// board is always complete (5 cards), so this is always a 7-card
// evaluation there, but it also works with a shorter board (used by
// MadeHand on the flop/turn).
func evaluate7(hole, board []Card) int32 {
	all := make([]Card, 0, len(hole)+len(board))
	all = append(all, hole...)
	all = append(all, board...)
	return defaultEvaluator.Evaluate(all)
}
