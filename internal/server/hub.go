package server

import (
	"log"
	"sync"

	"github.com/whoAngeel/pokerhub/internal/protocol"
	"github.com/whoAngeel/pokerhub/internal/table"
)

// DefaultRingCapacity is how many god-view envelopes Hub keeps for
// reconnect gap-filling (PLAN §5 / task: "last 5000 god-view envelopes").
const DefaultRingCapacity = 5000

// DefaultQueueSize is a connection's outgoing buffer, in envelopes, before
// it's considered slow and dropped (never block the game for one slow
// reader).
const DefaultQueueSize = 64

// Controller is what a spectator's control{pause|resume|step} acts on.
// table.Session satisfies this directly (see its Pause/Resume/Step).
type Controller interface {
	Pause()
	Resume()
	Step()
}

// SpeedController is optionally implemented by a Controller that can also
// adjust its pacing multiplier. table.Session (paced by real LLM/GPU
// latency, not a dial) does not implement it; replay.Player does. Hub logs
// and ignores control{speed} against a Controller that doesn't.
type SpeedController interface {
	SetSpeed(float64)
}

// subscriber is one live connection's delivery queue.
type subscriber struct {
	id      int64
	role    Role
	ch      chan protocol.Envelope
	dropped chan struct{} // closed by the hub if this subscriber is force-dropped (slow reader)
}

// Hub assigns seq+ts to every god-view table.Event it receives (it
// implements table.Sink), keeps a bounded ring buffer of the recent ones
// for reconnect, and fans each one out -- through Project -- to every live
// connection. A connection whose outgoing queue fills up is dropped rather
// than allowed to block the game.
type Hub struct {
	mu   sync.Mutex
	conv *protocol.Converter
	st   *protocol.TableState

	seq     int64
	ring    []protocol.Envelope
	ringCap int

	subs      map[int64]*subscriber
	nextSubID int64
}

// NewHub builds a Hub seeded with seats (PublicConfig.TableStateSeats()),
// using deadlineMS for every "turn" event's deadline_ms. ringCap<=0 uses
// DefaultRingCapacity.
func NewHub(seats []protocol.Seat, deadlineMS, ringCap int) *Hub {
	if ringCap <= 0 {
		ringCap = DefaultRingCapacity
	}
	return &Hub{
		conv:    protocol.NewConverter(deadlineMS),
		st:      protocol.NewTableState(seats),
		ringCap: ringCap,
		subs:    make(map[int64]*subscriber),
	}
}

// Emit implements table.Sink: it is the Hub's only way of learning about
// the game, called once per table.Event in order from the Session's single
// goroutine.
func (h *Hub) Emit(ev table.Event) {
	env, err := h.conv.Convert(ev)
	if err != nil {
		log.Printf("server: hub: convert %s: %v", ev.Type, err)
		return
	}
	h.publish(env)
}

// EmitEnvelope publishes an already-built god-view Envelope (its own
// Seq/TS are ignored and overwritten, like any other publish): this is how
// replay.Player re-publishes a recorded stream into a fresh Hub, since a
// recorded Envelope has no table.Event to hand to Emit anymore (Convert is
// one-directional).
func (h *Hub) EmitEnvelope(env protocol.Envelope) { h.publish(env) }

// publish assigns seq+ts, folds env into TableState, appends it to the ring
// (trimmed to ringCap), and fans it out to every current subscriber.
func (h *Hub) publish(env protocol.Envelope) {
	h.mu.Lock()
	h.seq++
	env = env.WithSeq(h.seq)
	if err := h.st.Apply(env); err != nil {
		log.Printf("server: hub: apply %s: %v", env.Type, err)
	}
	h.ring = append(h.ring, env)
	if len(h.ring) > h.ringCap {
		h.ring = h.ring[len(h.ring)-h.ringCap:]
	}
	subs := make([]*subscriber, 0, len(h.subs))
	for _, s := range h.subs {
		subs = append(subs, s)
	}
	h.mu.Unlock()

	for _, s := range subs {
		h.deliver(s, env)
	}
}

// deliver projects env for s's role and tries a non-blocking send; a full
// queue means a slow client, which gets dropped (never blocks the table).
func (h *Hub) deliver(s *subscriber, env protocol.Envelope) {
	proj, ok := Project(env, s.role)
	if !ok {
		return
	}
	select {
	case s.ch <- proj:
	default:
		h.dropSlow(s)
	}
}

func (h *Hub) dropSlow(s *subscriber) {
	h.mu.Lock()
	_, present := h.subs[s.id]
	if present {
		delete(h.subs, s.id)
	}
	h.mu.Unlock()
	if present {
		close(s.dropped)
	}
}

// Subscription is a connection's live handle on the Hub: Envelopes() yields
// projected envelopes as they're published; Dropped() closes if the hub
// force-dropped this subscriber as a slow reader; Close() unregisters a
// normal (voluntary) disconnect.
type Subscription struct {
	hub *Hub
	sub *subscriber
}

func (s *Subscription) Envelopes() <-chan protocol.Envelope { return s.sub.ch }
func (s *Subscription) Dropped() <-chan struct{}            { return s.sub.dropped }
func (s *Subscription) Role() Role                          { return s.sub.role }

// Close unregisters the subscription (no-op if it was already force-dropped).
func (s *Subscription) Close() {
	s.hub.mu.Lock()
	delete(s.hub.subs, s.sub.id)
	s.hub.mu.Unlock()
}

// Connect registers role as a new live connection and returns both the
// Subscription (for ongoing delivery) and the initial envelope(s) to send
// right away: PROTOCOL.md §1 "al conectar siempre se envía primero un
// snapshot", OR, when lastSeq is non-nil and the ring buffer still covers
// the gap, every projected envelope since lastSeq instead (§1 reconnect).
// Registration and snapshot/catch-up are computed under the same lock as
// publish's seq/ring bookkeeping, so there is no gap between "what the
// initial payload reflects" and "what the subscription starts receiving".
func (h *Hub) Connect(role Role, lastSeq *int64, queueSize int) (*Subscription, []protocol.Envelope) {
	if queueSize <= 0 {
		queueSize = DefaultQueueSize
	}

	h.mu.Lock()
	h.nextSubID++
	sub := &subscriber{
		id: h.nextSubID, role: role,
		ch:      make(chan protocol.Envelope, queueSize),
		dropped: make(chan struct{}),
	}
	h.subs[sub.id] = sub

	var catchUp []protocol.Envelope
	var useCatchUp bool
	if lastSeq != nil {
		catchUp, useCatchUp = h.catchUpLocked(*lastSeq)
	}
	var snapEnv protocol.Envelope
	var snapErr error
	if !useCatchUp {
		snapEnv, snapErr = h.st.Snapshot()
		snapEnv = snapEnv.WithSeq(h.seq)
	}
	h.mu.Unlock()

	subscription := &Subscription{hub: h, sub: sub}

	if useCatchUp {
		out := make([]protocol.Envelope, 0, len(catchUp))
		for _, e := range catchUp {
			if proj, ok := Project(e, role); ok {
				out = append(out, proj)
			}
		}
		return subscription, out
	}

	if snapErr != nil {
		log.Printf("server: hub: snapshot: %v", snapErr)
		return subscription, nil
	}
	if proj, ok := Project(snapEnv, role); ok {
		return subscription, []protocol.Envelope{proj}
	}
	return subscription, nil
}

// catchUpLocked returns every ring envelope with Seq>lastSeq, and whether
// the ring actually covers that gap (false when the oldest ring entry is
// already newer than lastSeq+1, i.e. something was trimmed, or lastSeq is
// somehow ahead of the hub's own seq). Caller must hold h.mu.
func (h *Hub) catchUpLocked(lastSeq int64) ([]protocol.Envelope, bool) {
	if lastSeq > h.seq {
		return nil, false
	}
	if lastSeq == h.seq {
		return nil, true // already current: zero missed events, no snapshot needed
	}
	if len(h.ring) == 0 {
		return nil, false
	}
	oldest := h.ring[0].Seq
	if lastSeq < oldest-1 {
		return nil, false // gap not covered: something aged out of the ring
	}
	out := make([]protocol.Envelope, 0, len(h.ring))
	for _, e := range h.ring {
		if e.Seq > lastSeq {
			out = append(out, e)
		}
	}
	return out, true
}

// Replay answers a mid-connection hello{last_seq} (PROTOCOL.md §1
// reconnect) the same way Connect's initial payload would, without
// re-registering the subscription: either the covered catch-up list, or a
// fresh snapshot when the ring buffer no longer covers the gap.
func (h *Hub) Replay(role Role, lastSeq int64) []protocol.Envelope {
	h.mu.Lock()
	catchUp, covered := h.catchUpLocked(lastSeq)
	var snapEnv protocol.Envelope
	var snapErr error
	if !covered {
		snapEnv, snapErr = h.st.Snapshot()
		snapEnv = snapEnv.WithSeq(h.seq)
	}
	h.mu.Unlock()

	if covered {
		out := make([]protocol.Envelope, 0, len(catchUp))
		for _, e := range catchUp {
			if proj, ok := Project(e, role); ok {
				out = append(out, proj)
			}
		}
		return out
	}
	if snapErr != nil {
		log.Printf("server: hub: replay snapshot: %v", snapErr)
		return nil
	}
	if proj, ok := Project(snapEnv, role); ok {
		return []protocol.Envelope{proj}
	}
	return nil
}

// State exposes the underlying TableState (e.g. for a /healthz-adjacent
// debug endpoint, or tests); it is the exact same instance Emit/publish
// mutate, so callers must not assume a read is isolated from concurrent
// Emit calls beyond TableState's own locking.
func (h *Hub) State() *protocol.TableState { return h.st }
