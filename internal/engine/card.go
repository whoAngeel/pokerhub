package engine

import "strings"

// rankOrder mirrors the chehsunliu/poker rank ordering: index 0 = "2" ...
// index 12 = "A".
const rankOrder = "23456789TJQKA"
const suitOrder = "shdc"

// rankValue returns 2..14 for a card's rank (14 = Ace).
func rankValue(c Card) int {
	if len(c) != 2 {
		return 0
	}
	return rankValueByte(c[0])
}

func rankValueByte(r byte) int {
	idx := strings.IndexByte(rankOrder, r)
	if idx < 0 {
		return 0
	}
	return idx + 2
}

func rankLetter(c Card) byte { return c[0] }

func suitLetter(c Card) byte { return c[1] }

// copyCards returns a fresh slice with the same contents, so callers can
// never mutate internal state through a returned slice.
func copyCards(cs []Card) []Card {
	if cs == nil {
		return nil
	}
	out := make([]Card, len(cs))
	copy(out, cs)
	return out
}
