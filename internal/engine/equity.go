package engine

import "math/rand/v2"

// equity estimates hero's win probability (ties split as a fractional
// share) against `opponents` random hands via Monte Carlo over `iters`
// samples. Works preflop through river (board may have 0, 3, 4 or 5
// cards already).
func equity(hole, board []Card, opponents, iters int, rng *rand.Rand) float64 {
	if iters <= 0 || opponents <= 0 || len(hole) != 2 {
		return 0
	}

	used := make(map[Card]bool, len(hole)+len(board))
	for _, c := range hole {
		used[c] = true
	}
	for _, c := range board {
		used[c] = true
	}

	remaining := make([]Card, 0, 52-len(used))
	for _, c := range newDeck() {
		if !used[c] {
			remaining = append(remaining, c)
		}
	}

	needBoard := 5 - len(board)
	if needBoard < 0 {
		needBoard = 0
	}
	k := 2*opponents + needBoard
	if k > len(remaining) {
		// Not enough cards for this many opponents; cap deterministically
		// rather than panicking on a misuse of the API.
		opponents = (len(remaining) - needBoard) / 2
		if opponents <= 0 {
			return 0
		}
		k = 2*opponents + needBoard
	}

	draw := make([]Card, len(remaining))
	fullBoard := make([]Card, 5)
	copy(fullBoard, board)
	codes := make([]int32, opponents+1)

	total := 0.0
	for it := 0; it < iters; it++ {
		copy(draw, remaining)
		partialShuffle(draw, k, rng)

		// Deal opponents first, then complete the board, matching the
		// slice we just partially shuffled.
		idx := 0
		oppHoles := make([][2]Card, opponents)
		for o := 0; o < opponents; o++ {
			oppHoles[o] = [2]Card{draw[idx], draw[idx+1]}
			idx += 2
		}
		for b := 0; b < needBoard; b++ {
			fullBoard[len(board)+b] = draw[idx]
			idx++
		}

		codes[0] = evaluate7(hole, fullBoard)
		for o := 0; o < opponents; o++ {
			codes[o+1] = evaluate7(oppHoles[o][:], fullBoard)
		}

		best := codes[0]
		for _, c := range codes[1:] {
			if c < best {
				best = c
			}
		}
		count := 0
		for _, c := range codes {
			if c == best {
				count++
			}
		}
		if codes[0] == best {
			total += 1.0 / float64(count)
		}
	}
	return total / float64(iters)
}
