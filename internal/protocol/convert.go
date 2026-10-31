package protocol

import (
	"fmt"
	"sort"
	"strings"

	"github.com/whoAngeel/pokerhub/internal/engine"
	"github.com/whoAngeel/pokerhub/internal/player"
	"github.com/whoAngeel/pokerhub/internal/table"
)

// defaultDeadlineMS is used by Converter when constructed with deadlineMS<=0.
const defaultDeadlineMS = 10000

// Converter turns table.Event (the orchestrator's internal event shape)
// into god-view protocol.Envelope (every hole card, every reasoning, no
// filtering: server.Project does that per connection). It is NOT a pure
// function of one event: table.Event doesn't carry a couple of fields
// PROTOCOL.md's wire shape needs (blind small/big + stack_after/pot_after,
// turn.deadline_ms), so Converter keeps the small amount of per-hand state
// needed to fill them in deterministically from the same event stream.
//
// Converter is not safe for concurrent use: it must see table.Event in the
// exact order table.Sink.Emit receives them (true of its one caller, Hub).
type Converter struct {
	deadlineMS int

	stacks       map[int]int // seat -> current stack, refreshed at hand_start
	pot          int
	blindsInHand int
}

// NewConverter builds a Converter. deadlineMS feeds every "turn" event's
// deadline_ms (PROTOCOL.md §4); deadlineMS<=0 falls back to 10000, matching
// table.Config.DecisionTimeoutMS's own default.
func NewConverter(deadlineMS int) *Converter {
	if deadlineMS <= 0 {
		deadlineMS = defaultDeadlineMS
	}
	return &Converter{deadlineMS: deadlineMS, stacks: map[int]int{}}
}

// Convert maps one table.Event to its god-view Envelope. Seq and TS are left
// zero: the hub assigns them (WithSeq) only once, right before fan-out, so
// every connection sees a consistent global ordering.
func (c *Converter) Convert(ev table.Event) (Envelope, error) {
	handID := ev.HandID
	var payload any

	switch ev.Type {
	case table.EvHandStart:
		c.stacks = make(map[int]int, len(ev.Stacks))
		for seat, stack := range ev.Stacks {
			c.stacks[seat] = stack
		}
		c.pot = 0
		c.blindsInHand = 0
		payload = HandStartPayload{
			HandID:     ev.HandID,
			Button:     ev.Button,
			SmallBlind: ev.SmallBlind,
			BigBlind:   ev.BigBlind,
			Seats:      handStartSeats(ev.Stacks, ev.SeatNames),
		}

	case table.EvBlind:
		c.blindsInHand++
		label := "big"
		if c.blindsInHand == 1 {
			label = "small"
		}
		c.stacks[ev.Seat] -= ev.Amount
		c.pot += ev.Amount
		payload = BlindPayload{
			Seat:       ev.Seat,
			Type:       label,
			Amount:     ev.Amount,
			StackAfter: c.stacks[ev.Seat],
			PotAfter:   c.pot,
		}

	case table.EvHoleCards:
		payload = HoleCardsPayload{Seat: ev.Seat, Cards: convertCards(ev.Cards)}

	case table.EvStreet:
		payload = StreetPayload{Street: string(ev.Street), Board: convertCards(ev.Board)}

	case table.EvTurn:
		payload = TurnPayload{
			Seat:         ev.Seat,
			ToCall:       ev.ToCall,
			DeadlineMS:   c.deadlineMS,
			LegalActions: convertLegalActions(ev.LegalActions),
		}

	case table.EvThinking:
		payload = ThinkingPayload{Seat: ev.Seat}

	case table.EvAction:
		payload = ActionPayload{
			Seat:       ev.Seat,
			Type:       string(ev.Action.Type),
			Amount:     ev.Action.Amount,
			StackAfter: ev.StackAfter,
			PotAfter:   ev.PotAfter,
		}
		c.stacks[ev.Seat] = ev.StackAfter
		c.pot = ev.PotAfter

	case table.EvChat:
		payload = ChatPayload{Seat: ev.Seat, Say: ev.Say}

	case table.EvReasoning:
		var hand, odds, reads, plan string
		if ev.Reasoning != nil {
			hand, odds, reads, plan = ev.Reasoning.Hand, ev.Reasoning.Odds, ev.Reasoning.Reads, ev.Reasoning.Plan
		}
		payload = ReasoningPayload{
			Seat: ev.Seat, Hand: hand, Odds: odds, Reads: reads, Plan: plan,
			Meta: convertMeta(ev.Meta),
		}

	case table.EvShowdown:
		payload = ShowdownPayload{Reveals: convertReveals(ev.Reveals)}

	case table.EvPotAwarded:
		payload = PotAwardedPayload{Pots: convertPots(ev.Pots)}

	case table.EvHandEnd:
		payload = HandEndPayload{HandID: ev.HandID, Seats: handEndSeats(ev.Stacks)}
		for seat, stack := range ev.Stacks {
			c.stacks[seat] = stack
		}

	case table.EvRebuy:
		c.stacks[ev.Seat] = ev.Amount
		payload = RebuyPayload{Seat: ev.Seat, Amount: ev.Amount, StackAfter: ev.Amount}

	case table.EvAgentError:
		fallback := ev.Meta.Fallback || strings.HasPrefix(ev.Cause, "illegal_action")
		payload = AgentErrorPayload{
			Seat: ev.Seat, Error: ev.Cause, Fallback: fallback, Warnings: ev.Meta.Warnings,
		}

	default:
		return Envelope{}, fmt.Errorf("protocol: unknown table.Event type %q", ev.Type)
	}

	return NewEnvelope(string(ev.Type), &handID, payload)
}

func handStartSeats(stacks map[int]int, names map[int]string) []HandStartSeat {
	seats := sortedSeatKeys(stacks)
	out := make([]HandStartSeat, len(seats))
	for i, seat := range seats {
		out[i] = HandStartSeat{Seat: seat, Name: names[seat], Stack: stacks[seat]}
	}
	return out
}

func handEndSeats(stacks map[int]int) []HandEndSeat {
	seats := sortedSeatKeys(stacks)
	out := make([]HandEndSeat, len(seats))
	for i, seat := range seats {
		out[i] = HandEndSeat{Seat: seat, Stack: stacks[seat]}
	}
	return out
}

func sortedSeatKeys(m map[int]int) []int {
	out := make([]int, 0, len(m))
	for seat := range m {
		out = append(out, seat)
	}
	sort.Ints(out)
	return out
}

func convertCards(cards []engine.Card) []Card {
	if cards == nil {
		return nil
	}
	out := make([]Card, len(cards))
	for i, c := range cards {
		out[i] = Card(c)
	}
	return out
}

func convertLegalActions(legal []player.LegalAction) []LegalAction {
	if legal == nil {
		return nil
	}
	out := make([]LegalAction, len(legal))
	for i, la := range legal {
		out[i] = LegalAction{Type: la.Type, Amount: la.Amount, Min: la.Min, Max: la.Max}
	}
	return out
}

func convertReveals(reveals []engine.Reveal) []Reveal {
	out := make([]Reveal, len(reveals))
	for i, r := range reveals {
		out[i] = Reveal{Seat: r.Seat, Cards: convertCards(r.Cards), Rank: r.Rank}
	}
	return out
}

func convertPots(pots []engine.Pot) []Pot {
	out := make([]Pot, len(pots))
	for i, p := range pots {
		winners := make([]int, len(p.Winners))
		copy(winners, p.Winners)
		out[i] = Pot{Amount: p.Amount, Winners: winners}
	}
	return out
}

func convertMeta(m player.DecisionMeta) Meta {
	return Meta{
		Provider:  m.Provider,
		Model:     m.Model,
		LatencyMS: m.LatencyMS,
		TokensOut: m.TokensOut,
		Retries:   m.Retries,
		Fallback:  m.Fallback,
		Warnings:  m.Warnings,
	}
}
