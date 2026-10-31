package table

import "github.com/whoAngeel/pokerhub/internal/engine"

// emitEngineEvents feeds the memory tracker and the action log, and emits a
// table.Event for every engine.Event, in order. It is the ONLY place table
// forwards engine-produced data outward, so it's also the natural place to
// double check nothing beyond what's already public (engine.Event never
// carries hole cards except the Reveals showdown already made public) leaks.
func (s *Session) emitEngineEvents(events []engine.Event) {
	for _, ev := range events {
		s.stats.Record(ev)

		switch ev.Kind {
		case engine.EvHandStart:
			s.emit(Event{
				Type:       EvHandStart,
				Button:     ev.Button,
				SmallBlind: s.cfg.SmallBlind,
				BigBlind:   s.cfg.BigBlind,
				Stacks:     ev.Stacks,
				SeatNames:  s.seatNames(),
			})

		case engine.EvBlind:
			s.emit(Event{
				Type:     EvBlind,
				Seat:     ev.Seat,
				SeatName: s.names[ev.Seat],
				Amount:   ev.Action.Amount,
			})

		case engine.EvHoleCards:
			s.emit(Event{
				Type:     EvHoleCards,
				Seat:     ev.Seat,
				SeatName: s.names[ev.Seat],
				Cards:    ev.Cards,
			})

		case engine.EvAction:
			s.log.recordAction(s.names[ev.Seat], ev.Seat, ev.Action)
			s.emit(Event{
				Type:       EvAction,
				Seat:       ev.Seat,
				SeatName:   s.names[ev.Seat],
				Action:     ev.Action,
				StackAfter: ev.StackAfter,
				PotAfter:   ev.PotAfter,
			})

		case engine.EvStreet:
			s.log.openStreet(ev.Street)
			s.emit(Event{Type: EvStreet, Street: ev.Street, Board: ev.Board})

		case engine.EvShowdown:
			s.emit(Event{Type: EvShowdown, Reveals: ev.Reveals})

		case engine.EvPotAwarded:
			s.emit(Event{Type: EvPotAwarded, Pots: ev.Pots})

		case engine.EvHandEnd:
			for seat, amt := range ev.Stacks {
				s.stacks[seat] = amt
			}
			s.emit(Event{Type: EvHandEnd, Stacks: ev.Stacks})
		}
	}
}

func (s *Session) seatNames() map[int]string {
	out := make(map[int]string, len(s.names))
	for i, n := range s.names {
		out[i] = n
	}
	return out
}
