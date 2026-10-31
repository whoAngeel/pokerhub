package table

import (
	"context"
	"time"

	"github.com/whoAngeel/pokerhub/internal/memory"
)

// noteUpdateTimeout bounds a single notes-update call independently of
// Run's own ctx (PLAN §7: a memory update must never block or break the
// game). notesMaxTokens is small (~200), so this is generous.
const noteUpdateTimeout = 8 * time.Second

// SetMemoryAgents wires per-seat memory-notes capability into the session
// (PLAN §7 / docs/PROTOCOL.md "memory_update"): agents[i].Provider == nil
// means seat i either isn't an LLM seat or has memory notes disabled
// (table.memory_notes: false), and Session skips it entirely. Call this
// once, before Run. A nil/empty agents disables the feature for every seat
// (the zero value of Session already behaves this way).
func (s *Session) SetMemoryAgents(agents []memory.AgentInfo) {
	s.memAgents = agents
}

// maybeUpdateMemory is called once per hand, right after hand_end. It is a
// no-op unless the hand just finished reached showdown (s.lastReveals is
// only ever non-empty right after an EvShowdown, reset to nil on the next
// EvHandStart) and at least one seat has memory notes enabled. Otherwise it
// enqueues exactly one llm.PriorityLow call per enabled LLM seat, each in
// its own goroutine tracked by s.noteWG so Run can wait for (or let ctx
// cancel) every in-flight update before returning.
func (s *Session) maybeUpdateMemory(ctx context.Context) {
	if len(s.memAgents) == 0 || len(s.lastReveals) == 0 {
		return
	}

	summary := memory.HandSummary(s.log.String(), s.lastReveals, s.names)
	seats := s.seatNames()
	handID := s.handID

	for _, info := range s.memAgents {
		if info.Provider == nil {
			continue
		}
		observer := info.Seat
		current := s.notesFor(observer)

		s.noteWG.Add(1)
		go func(info memory.AgentInfo, current map[int][]string) {
			defer s.noteWG.Done()

			updateCtx, cancel := context.WithTimeout(ctx, noteUpdateTimeout)
			defer cancel()

			updated, err := memory.Update(updateCtx, info, summary, seats, current)
			if err != nil || len(updated) == 0 {
				// Keep the old notes, silently: a bad/empty/erroring
				// response never blocks or breaks the game (PLAN §7),
				// and the mock provider's own default ("{}") is exactly
				// this shape, so --mock runs are silent by design.
				return
			}

			s.mergeNotes(observer, updated)
			s.emit(Event{
				Type:        EvMemoryUpdate,
				HandID:      handID,
				Seat:        observer,
				SeatName:    s.names[observer],
				MemoryNotes: updated,
			})
		}(info, current)
	}
}

// notesFor returns a defensive copy of everything observer currently knows
// about every rival (rival seat -> notes), for feeding back into the next
// notes-update prompt as "current notes".
func (s *Session) notesFor(observer int) map[int][]string {
	s.noteMu.Lock()
	defer s.noteMu.Unlock()
	src := s.notes[observer]
	if len(src) == 0 {
		return nil
	}
	out := make(map[int][]string, len(src))
	for seat, notes := range src {
		out[seat] = append([]string{}, notes...)
	}
	return out
}

// mergeNotes replaces (never appends to) observer's notes about every
// rival seat present in updates, leaving every other rival's notes alone
// (PLAN §7: "sustitución, no acumulación", per rival). A self-note (a seat
// somehow "observing" itself) is dropped rather than stored.
func (s *Session) mergeNotes(observer int, updates map[int][]string) {
	s.noteMu.Lock()
	defer s.noteMu.Unlock()
	if s.notes == nil {
		s.notes = map[int]map[int][]string{}
	}
	if s.notes[observer] == nil {
		s.notes[observer] = map[int][]string{}
	}
	for rival, notes := range updates {
		if rival == observer {
			continue
		}
		s.notes[observer][rival] = notes
	}
}

// noteFor is what decisionview.go's opponentsFor calls: observer's current
// notes about rival, or nil if there are none yet. This is the ONLY read
// path memory notes take into a DecisionView, so it's also the one place
// that enforces privacy: a seat only ever sees s.notes[itsOwnSeat][...],
// never another observer's notes about the same rival.
func (s *Session) noteFor(observer, rival int) []string {
	s.noteMu.Lock()
	defer s.noteMu.Unlock()
	notes := s.notes[observer][rival]
	if len(notes) == 0 {
		return nil
	}
	return append([]string{}, notes...)
}
