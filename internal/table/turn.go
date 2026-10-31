package table

import (
	"context"
	"fmt"
	"time"

	"github.com/whoAngeel/pokerhub/internal/engine"
	"github.com/whoAngeel/pokerhub/internal/player"
)

// takeTurn runs one full decision for seat: builds its DecisionView, emits
// turn/thinking, calls Decide under a decision-timeout context, applies the
// result (falling back to check-if-legal-else-fold and an agent_error event
// if it's somehow illegal), and emits reasoning/chat.
func (s *Session) takeTurn(hand *engine.Hand, seat int) {
	pub := hand.Public()
	legal := hand.LegalActions(seat)

	s.emit(Event{
		Type:         EvTurn,
		Seat:         seat,
		SeatName:     s.names[seat],
		ToCall:       pub.ToCall,
		LegalActions: toPlayerLegalActions(legal),
	})

	view := s.buildDecisionView(hand, pub, seat, legal)

	s.emit(Event{Type: EvThinking, Seat: seat, SeatName: s.names[seat]})

	decisionCtx, cancel := context.WithTimeout(context.Background(), s.decisionTimeout())
	decision := s.players[seat].Decide(decisionCtx, view)
	cancel()

	if cause := fallbackCause(decision.Meta); cause != "" {
		s.emit(Event{Type: EvAgentError, Seat: seat, SeatName: s.names[seat], Cause: cause, Meta: decision.Meta})
	}
	if decision.Reasoning != nil {
		s.emit(Event{Type: EvReasoning, Seat: seat, SeatName: s.names[seat], Reasoning: decision.Reasoning, Meta: decision.Meta})
	}
	if decision.Say != "" {
		s.appendChat(seat, decision.Say)
		s.emit(Event{Type: EvChat, Seat: seat, SeatName: s.names[seat], Say: decision.Say})
	}

	action := engine.Action{Type: engine.ActionType(decision.Action.Type), Amount: decision.Action.Amount}
	events, err := hand.Apply(seat, action)
	if err != nil {
		fb := fallbackAction(legal)
		s.emit(Event{
			Type: EvAgentError, Seat: seat, SeatName: s.names[seat],
			Cause: "illegal_action: " + err.Error(), AttemptedAction: action,
		})
		events, err = hand.Apply(seat, fb)
		if err != nil {
			// check-if-legal-else-fold must always be legal when this seat
			// is the one ToAct named; a second failure is an engine bug,
			// not something the table can paper over.
			panic(fmt.Sprintf("table: fallback action also illegal for seat %d: %v", seat, err))
		}
	}

	s.emitEngineEvents(events)
}

func (s *Session) decisionTimeout() time.Duration {
	ms := s.cfg.DecisionTimeoutMS
	if ms <= 0 {
		ms = 10000
	}
	return time.Duration(ms) * time.Millisecond
}

// appendChat records say in the rolling chat buffer, capped to chatCapacity.
func (s *Session) appendChat(seat int, say string) {
	s.chat = append(s.chat, player.ChatLine{Seat: seat, Say: say})
	if len(s.chat) > chatCapacity {
		s.chat = s.chat[len(s.chat)-chatCapacity:]
	}
}

// fallbackAction is check-if-legal-else-fold (PLAN §4.3 point 5 / this
// task's table-level safety net for an action that's illegal even after
// agent.ParseAndNormalize already ran).
func fallbackAction(legal []engine.LegalAction) engine.Action {
	for _, la := range legal {
		if la.Type == engine.Check {
			return engine.Action{Type: engine.Check}
		}
	}
	return engine.Action{Type: engine.Fold}
}

// fallbackCause renders Decision.Meta into an agent_error Cause string, or
// "" when there's nothing to report. Per docs/PROTOCOL.md, agent_error is
// reserved for an actual fallback (Meta.Fallback == true) or a table-level
// illegal-action override (see the separate "illegal_action: " emit below);
// a decision that only carries normalization Warnings (clamped_amount,
// normalized:bet->raise, ...) without falling back is NOT an agent_error --
// those warnings still ride along on the reasoning event's Meta, visible to
// spectators there.
func fallbackCause(m player.DecisionMeta) string {
	if !m.Fallback {
		return ""
	}
	if len(m.Warnings) == 0 {
		return "fallback"
	}
	cause := m.Warnings[0]
	for _, w := range m.Warnings[1:] {
		cause += "," + w
	}
	return cause
}

func toPlayerLegalActions(legal []engine.LegalAction) []player.LegalAction {
	out := make([]player.LegalAction, len(legal))
	for i, la := range legal {
		out[i] = player.LegalAction{Type: string(la.Type), Amount: la.Amount, Min: la.Min, Max: la.Max}
	}
	return out
}
