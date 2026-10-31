package table

import (
	"math"

	"github.com/whoAngeel/pokerhub/internal/engine"
	"github.com/whoAngeel/pokerhub/internal/player"
)

// buildDecisionView is the ONLY place that turns engine state into what a
// Player sees. Its only sources are hand.ViewFor(seat) (own hole cards +
// public state) and hand.LegalActions(seat): it never reaches into another
// seat's hole cards. See table_test.go's TestNoOpponentHoleCardLeak.
func (s *Session) buildDecisionView(hand *engine.Hand, pub engine.PublicState, seat int, legal []engine.LegalAction) player.DecisionView {
	pv := hand.ViewFor(seat)

	me := player.Me{Seat: seat, Name: s.names[seat], Hole: cardsToStrings(pv.Hole)}
	for _, ps := range pub.Seats {
		if ps.Seat == seat {
			me.Stack = ps.Stack
			me.InPot = ps.TotalInPot
			break
		}
	}

	helpers := player.Helpers{
		MadeHand: engine.MadeHand(pv.Hole, pub.Board),
		Equity:   round2(engine.Equity(pv.Hole, pub.Board, activeOpponentCount(pub, seat), s.equityIters(), s.rng)),
		PotOdds:  round2(potOdds(pub.ToCall, pub.Pot)),
	}

	return player.DecisionView{
		HandID:       pub.HandID,
		Street:       string(pub.Street),
		Button:       pub.Button,
		You:          me,
		Board:        cardsToStrings(pub.Board),
		Pot:          pub.Pot,
		ToCall:       pub.ToCall,
		Helpers:      helpers,
		ActionLog:    s.log.String(),
		Opponents:    s.opponentsFor(pub, seat),
		Chat:         s.recentChat(),
		LegalActions: toPlayerLegalActions(legal),
	}
}

func (s *Session) opponentsFor(pub engine.PublicState, seat int) []player.Opponent {
	out := make([]player.Opponent, 0, len(pub.Seats)-1)
	for _, ps := range pub.Seats {
		if ps.Seat == seat {
			continue
		}
		stats := s.stats.Stats(ps.Seat)
		stats.VPIP, stats.PFR, stats.AF = round2(stats.VPIP), round2(stats.PFR), round2(stats.AF)
		out = append(out, player.Opponent{
			Seat:       ps.Seat,
			Name:       s.names[ps.Seat],
			Stack:      ps.Stack,
			Status:     string(ps.Status),
			LastAction: s.log.lastActionFor(ps.Seat),
			Stats:      &stats,
		})
	}
	return out
}

// recentChat returns a copy of the last chatCapacity lines, so callers
// can't mutate Session's buffer through the DecisionView.
func (s *Session) recentChat() []player.ChatLine {
	if len(s.chat) == 0 {
		return nil
	}
	out := make([]player.ChatLine, len(s.chat))
	copy(out, s.chat)
	return out
}

func (s *Session) equityIters() int {
	if s.cfg.EquityIters <= 0 {
		return 1000
	}
	return s.cfg.EquityIters
}

// activeOpponentCount counts seats other than seat that haven't folded
// (still eligible to contest the pot), for engine.Equity's "vs N opponents".
// Clamped to at least 1 so Equity never divides by/samples zero opponents.
func activeOpponentCount(pub engine.PublicState, seat int) int {
	n := 0
	for _, ps := range pub.Seats {
		if ps.Seat != seat && ps.Status != engine.Folded {
			n++
		}
	}
	if n < 1 {
		n = 1
	}
	return n
}

// potOdds = to_call / (pot + to_call), 0 when there's nothing to call
// (PLAN §3's PotOdds helper).
func potOdds(toCall, pot int) float64 {
	if toCall <= 0 {
		return 0
	}
	return float64(toCall) / float64(pot+toCall)
}

// round2 keeps prompt numbers short: small models copy long floats verbatim
// and mangle them (seen: "pot_odds=0.0.14958333333333335").
func round2(x float64) float64 {
	return math.Round(x*100) / 100
}

func cardsToStrings(cards []engine.Card) []string {
	if cards == nil {
		return nil
	}
	out := make([]string, len(cards))
	for i, c := range cards {
		out[i] = string(c)
	}
	return out
}
