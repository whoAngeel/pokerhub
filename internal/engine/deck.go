package engine

import "math/rand/v2"

// newDeck returns the 52 standard cards in a fixed canonical order
// (rank-major, suit-minor). Shuffling is the caller's responsibility.
func newDeck() []Card {
	d := make([]Card, 0, 52)
	for i := 0; i < len(rankOrder); i++ {
		for j := 0; j < len(suitOrder); j++ {
			d = append(d, Card([]byte{rankOrder[i], suitOrder[j]}))
		}
	}
	return d
}

// shuffleDeck performs an in-place Fisher-Yates shuffle using rng, so the
// same seed always produces the same order.
func shuffleDeck(d []Card, rng *rand.Rand) {
	for i := len(d) - 1; i > 0; i-- {
		j := rng.IntN(i + 1)
		d[i], d[j] = d[j], d[i]
	}
}

// partialShuffle randomizes only the first k positions of d (Fisher-Yates
// restricted to a prefix); the remaining elements are left in whatever
// order but every element has equal probability of landing in the first
// k slots. Used by Equity, which only needs a handful of random cards
// per iteration and would otherwise waste time shuffling an entire
// ~45-card remainder.
func partialShuffle(d []Card, k int, rng *rand.Rand) {
	n := len(d)
	if k > n {
		k = n
	}
	for i := 0; i < k; i++ {
		j := i + rng.IntN(n-i)
		d[i], d[j] = d[j], d[i]
	}
}
