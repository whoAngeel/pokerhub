package engine

// rankWordEs is the Spanish plural used in "pareja de <palabra>".
var rankWordEs = map[byte]string{
	'2': "doses", '3': "treses", '4': "cuatros", '5': "cincos",
	'6': "seises", '7': "sietes", '8': "ochos", '9': "nueves",
	'T': "dieces", 'J': "jotas", 'Q': "reinas", 'K': "reyes", 'A': "ases",
}

// madeHand describes hole+board in short Spanish, per CLAUDE.md's rule
// that table text is Spanish. See api.go's MadeHand doc comment for the
// preflop phrasing contract.
func madeHand(hole, board []Card) string {
	if len(hole) != 2 {
		return ""
	}
	if len(board) == 0 {
		return preflopDescription(hole)
	}

	all := make([]Card, 0, len(hole)+len(board))
	all = append(all, hole...)
	all = append(all, board...)

	code := defaultEvaluator.Evaluate(all)
	class := defaultEvaluator.Class(code)
	desc := postflopDescription(class, all)

	// Only worth noting draws on incomplete boards, and only when the
	// made hand isn't already strong (keeps the text short and useful).
	if len(board) < 5 && class >= 6 {
		if note := drawNote(all); note != "" {
			desc += " " + note
		}
	}
	return desc
}

func preflopDescription(hole []Card) string {
	if hole[0][0] == hole[1][0] {
		return "pareja de " + rankWordEs[hole[0][0]]
	}
	hi, lo := hole[0], hole[1]
	if rankValue(hole[1]) > rankValue(hole[0]) {
		hi, lo = hole[1], hole[0]
	}
	letters := string(rankLetter(hi)) + string(rankLetter(lo))
	if suitLetter(hole[0]) == suitLetter(hole[1]) {
		return letters + " del mismo palo"
	}
	return letters + " de distinto palo"
}

func postflopDescription(class int32, all []Card) string {
	if class == 8 { // pair
		if word := pairedRankWord(all); word != "" {
			return "pareja de " + word
		}
	}
	if name, ok := handClassNameEs[class]; ok {
		return name
	}
	return "carta alta"
}

// pairedRankWord finds the rank that is paired (exactly two of a kind)
// among all cards, picking the higher one if somehow more than one
// qualifies (shouldn't happen when class==8, kept as a safe fallback).
func pairedRankWord(all []Card) string {
	counts := map[byte]int{}
	for _, c := range all {
		counts[c[0]]++
	}
	var best byte
	bestVal := -1
	for r, n := range counts {
		if n == 2 && rankValueByte(r) > bestVal {
			bestVal = rankValueByte(r)
			best = r
		}
	}
	if best == 0 {
		return ""
	}
	return rankWordEs[best]
}

// drawNote reports cheap, structural draw heuristics: a 4-flush or an
// open-ended straight draw (4 consecutive ranks with live cards on both
// ends). It does not check remaining-deck availability; it's a cheap
// hint, not an odds calculation (Equity is the real number).
func drawNote(all []Card) string {
	flush := hasFlushDraw(all)
	straight := hasOpenEndedDraw(all)
	switch {
	case flush && straight:
		return "+ proyecto de color y escalera abierta"
	case flush:
		return "+ proyecto de color"
	case straight:
		return "+ proyecto de escalera abierta"
	default:
		return ""
	}
}

func hasFlushDraw(all []Card) bool {
	counts := map[byte]int{}
	for _, c := range all {
		counts[c[1]]++
		if counts[c[1]] == 4 {
			return true
		}
	}
	return false
}

func hasOpenEndedDraw(all []Card) bool {
	// present[v] for v in 1..14; Ace counts as both 1 (wheel-low) and 14.
	var present [15]bool
	for _, c := range all {
		v := rankValue(c)
		present[v] = true
		if v == 14 {
			present[1] = true
		}
	}
	for start := 1; start <= 10; start++ {
		if present[start] && present[start+1] && present[start+2] && present[start+3] {
			lowOpen := start-1 >= 1
			highOpen := start+4 <= 14
			if lowOpen && highOpen {
				return true
			}
		}
	}
	return false
}
