package table

import (
	"fmt"
	"strings"

	"github.com/whoAngeel/pokerhub/internal/engine"
)

// actionLog builds the CURRENT hand's compact, one-segment-per-street log
// (PLAN §4.1: "preflop: Toro raise 30, Abuela call | flop: ..."), and tracks
// each seat's most recent action this hand for the DecisionView opponents
// list ("last_action").
type actionLog struct {
	street     engine.Street
	current    []string
	segments   []string
	lastAction map[int]string
}

func newActionLog() *actionLog {
	return &actionLog{lastAction: map[int]string{}}
}

// reset clears all state for a new hand.
func (l *actionLog) reset() {
	l.street = engine.Preflop
	l.current = nil
	l.segments = nil
	l.lastAction = map[int]string{}
}

// recordAction appends one action to the current street's segment and
// updates that seat's last-action token.
func (l *actionLog) recordAction(name string, seat int, a engine.Action) {
	token := actionToken(a)
	l.current = append(l.current, name+" "+token)
	l.lastAction[seat] = token
}

// openStreet flushes the street that just closed and starts the next one.
func (l *actionLog) openStreet(st engine.Street) {
	l.flush()
	l.street = st
	l.current = nil
}

func (l *actionLog) flush() {
	if len(l.current) == 0 {
		return
	}
	l.segments = append(l.segments, string(l.street)+": "+strings.Join(l.current, ", "))
	l.current = nil
}

// String renders every closed segment plus whatever's accumulated for the
// street in progress, e.g. "preflop: Toro raise 30, Abuela call | flop: Toro bet 40".
func (l *actionLog) String() string {
	segs := l.segments
	if len(l.current) > 0 {
		segs = append(append([]string{}, segs...), string(l.street)+": "+strings.Join(l.current, ", "))
	}
	return strings.Join(segs, " | ")
}

// lastActionFor returns seat's most recent action token this hand ("" if
// the seat hasn't acted yet this hand).
func (l *actionLog) lastActionFor(seat int) string {
	return l.lastAction[seat]
}

// actionToken renders one action compactly: amount shown only for bet/raise/
// all_in (fold/check/call never carry a useful amount in the log, matching
// PLAN §4.1's example where "call" appears bare).
func actionToken(a engine.Action) string {
	switch a.Type {
	case engine.Bet, engine.Raise, engine.AllIn:
		return fmt.Sprintf("%s %d", a.Type, a.Amount)
	default:
		return string(a.Type)
	}
}
