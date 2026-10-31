//go:build race

package engine

// raceEnabled is true when the test binary was built with -race. The
// race detector instruments every memory access, which can slow down
// hot loops like Equity's Monte Carlo by an order of magnitude, so
// timing-sensitive assertions need a looser budget under it.
const raceEnabled = true
