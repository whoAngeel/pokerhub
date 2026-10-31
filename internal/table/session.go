package table

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/whoAngeel/pokerhub/internal/engine"
	"github.com/whoAngeel/pokerhub/internal/memory"
	"github.com/whoAngeel/pokerhub/internal/player"
)

// Config is everything Session needs beyond the players themselves. It
// mirrors config.Table but lives here so table never imports internal/config
// (config imports table-adjacent packages, not the other way around, and
// table stays usable from a unit test without a YAML file on disk).
type Config struct {
	SmallBlind        int
	BigBlind          int
	BuyIn             int
	Seed              int64 // 0 = time-based seed, chosen HERE (table), never inside internal/engine
	ActionDelayMS     int
	DecisionTimeoutMS int
	EquityIters       int
}

// chatCapacity bounds how many recent chat lines ride along in every
// DecisionView (PLAN §4.1: "últimos N mensajes de chat").
const chatCapacity = 5

// Session is the cash-game orchestrator: seats, button rotation, auto
// rebuy, and the per-hand turn loop. It is driven by Run and controlled by
// Pause/Resume/Step, all safe for concurrent use (Run executes on its own
// goroutine; Pause/Resume/Step are typically called from another one, e.g.
// a WS control handler or a terminal keypress listener).
type Session struct {
	cfg     Config
	players []player.Player // index == seat
	names   []string

	sink     Sink
	warmedUp bool
	stats    *memory.Tracker
	rng      *rand.Rand

	stacks map[int]int
	button int
	handID int
	chat   []player.ChatLine

	// control (Pause/Resume/Step), guarded by mu
	mu     sync.Mutex
	paused bool
	stepCh chan struct{}

	log *actionLog
}

// NewSession builds a Session over players (index == seat, 2-9 entries),
// starting every seat at cfg.BuyIn chips. Seed 0 picks a time-based seed
// (fine here: table is the orchestrator, not internal/engine, which stays
// pure and deterministic given whatever *rand.Rand it's handed).
func NewSession(cfg Config, players []player.Player, sink Sink) (*Session, error) {
	if len(players) < 2 || len(players) > 9 {
		return nil, fmt.Errorf("table: need 2-9 players, got %d", len(players))
	}
	seed := uint64(cfg.Seed)
	if cfg.Seed == 0 {
		seed = uint64(time.Now().UnixNano())
	}

	names := make([]string, len(players))
	stacks := make(map[int]int, len(players))
	for i, p := range players {
		names[i] = p.Name()
		stacks[i] = cfg.BuyIn
	}

	return &Session{
		cfg:     cfg,
		players: players,
		names:   names,
		sink:    sink,
		stats:   memory.NewTracker(),
		rng:     rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15)),
		stacks:  stacks,
		button:  0,
		handID:  0,
		stepCh:  make(chan struct{}, 1),
		log:     newActionLog(),
	}, nil
}

// Stacks returns a snapshot of every seat's current chip stack.
func (s *Session) Stacks() map[int]int {
	out := make(map[int]int, len(s.stacks))
	for seat, amt := range s.stacks {
		out[seat] = amt
	}
	return out
}

// Pause stops the turn loop from starting any new decision until Resume or
// Step is called. It takes effect between turns, never mid-Apply.
func (s *Session) Pause() {
	s.mu.Lock()
	s.paused = true
	s.mu.Unlock()
}

// Resume cancels a Pause.
func (s *Session) Resume() {
	s.mu.Lock()
	s.paused = false
	s.mu.Unlock()
	select {
	case s.stepCh <- struct{}{}:
	default:
	}
}

// Step lets exactly one more turn through even while paused.
func (s *Session) Step() {
	select {
	case s.stepCh <- struct{}{}:
	default:
	}
}

// waitIfPaused blocks while Paused, until Step/Resume or ctx cancellation.
func (s *Session) waitIfPaused(ctx context.Context) error {
	for {
		s.mu.Lock()
		paused := s.paused
		s.mu.Unlock()
		if !paused {
			return nil
		}
		select {
		case <-s.stepCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Run plays up to `hands` hands (hands <= 0: until ctx is cancelled),
// stopping cleanly after the CURRENT hand
// completes if ctx is cancelled (a hand already in progress is never cut
// off mid-way: per-decision timeouts are independent of ctx, see decide()).
func (s *Session) Run(ctx context.Context, hands int) error {
	s.warmup(ctx)
	for i := 0; hands <= 0 || i < hands; i++ {
		if err := ctx.Err(); err != nil {
			return nil
		}
		s.playHand(ctx)
		if err := ctx.Err(); err != nil {
			return nil
		}
	}
	return nil
}

// warmup lets model-backed players load before the first hand (cold start
// measured at ~5 s vs ~1.2 s warm). Players run sequentially: they usually
// share one local model behind the GPU queue anyway.
func (s *Session) warmup(ctx context.Context) {
	if s.warmedUp {
		return
	}
	s.warmedUp = true
	for _, p := range s.players {
		if w, ok := p.(player.Warmer); ok {
			wctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			_ = w.Warmup(wctx)
			cancel()
		}
	}
}

// playHand runs exactly one hand to completion: rebuy, deal, turn loop,
// showdown/award, then updates stacks and rotates the button.
func (s *Session) playHand(ctx context.Context) {
	s.handID++
	s.rebuyIfNeeded()

	seats := make([]engine.SeatState, len(s.players))
	for seat := range s.players {
		seats[seat] = engine.SeatState{Seat: seat, Stack: s.stacks[seat], Status: engine.Active}
	}

	hand, initEvents := engine.NewHand(engine.HandConfig{
		HandID:     s.handID,
		SmallBlind: s.cfg.SmallBlind,
		BigBlind:   s.cfg.BigBlind,
	}, seats, s.button, s.rng)

	s.log.reset()
	s.emitEngineEvents(initEvents)

	for {
		seat, ok := hand.ToAct()
		if !ok {
			break
		}
		if err := s.waitIfPaused(ctx); err != nil {
			// ctx cancelled while paused mid-hand: fall through and keep
			// playing the hand to completion rather than leaving it stuck,
			// honoring "never cut off mid-hand" at the cost of ignoring the
			// pause just this once.
			_ = err
		}
		s.takeTurn(hand, seat)
		s.pace()
	}

	// hand.ToAct() going false means the hand is fully resolved: Apply's
	// last batch of events (pot_awarded, hand_end) was already emitted by
	// the final takeTurn call.
	s.button = (s.button + 1) % len(s.players)
}

func (s *Session) rebuyIfNeeded() {
	for seat := range s.players {
		if s.stacks[seat] > 0 {
			continue
		}
		s.stacks[seat] = s.cfg.BuyIn
		s.emit(Event{
			Type:     EvRebuy,
			HandID:   s.handID,
			Seat:     seat,
			SeatName: s.names[seat],
			Amount:   s.cfg.BuyIn,
		})
	}
}

func (s *Session) pace() {
	if s.cfg.ActionDelayMS <= 0 {
		return
	}
	time.Sleep(time.Duration(s.cfg.ActionDelayMS) * time.Millisecond)
}

func (s *Session) emit(e Event) {
	if e.HandID == 0 {
		e.HandID = s.handID
	}
	s.sink.Emit(e)
}
