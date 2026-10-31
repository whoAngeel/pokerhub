package replay

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/whoAngeel/pokerhub/internal/protocol"
	"github.com/whoAngeel/pokerhub/internal/server"
)

// maxGap caps any single inter-envelope wait at 3s/speed (task spec):
// a recording with a long real-world pause (e.g. across a slow LLM call)
// shouldn't make a demo sit idle for the original duration.
const maxGap = 3 * time.Second

// Player reads a .jsonl recording made by Recorder and re-publishes each
// envelope into a server.Hub, honoring the original gaps between
// consecutive envelopes' ts (divided by Speed, capped at maxGap/Speed).
// It satisfies server.Controller (Pause/Resume/Step) and
// server.SpeedController (SetSpeed), so docs/PROTOCOL.md's `control`
// message works against a replay exactly like it does against a live
// table.Session.
type Player struct {
	path string
	loop bool

	mu     sync.Mutex
	speed  float64
	paused bool
	stepCh chan struct{}
}

// NewPlayer builds a Player over path. speed<=0 is treated as 1 (real
// time); loop replays the file again from the top once it ends, forever
// (until ctx is cancelled).
func NewPlayer(path string, speed float64, loop bool) *Player {
	if speed <= 0 {
		speed = 1
	}
	return &Player{path: path, loop: loop, speed: speed, stepCh: make(chan struct{}, 1)}
}

// Pause implements server.Controller.
func (p *Player) Pause() {
	p.mu.Lock()
	p.paused = true
	p.mu.Unlock()
}

// Resume implements server.Controller.
func (p *Player) Resume() {
	p.mu.Lock()
	p.paused = false
	p.mu.Unlock()
	select {
	case p.stepCh <- struct{}{}:
	default:
	}
}

// Step implements server.Controller: lets exactly one more envelope through
// even while paused.
func (p *Player) Step() {
	select {
	case p.stepCh <- struct{}{}:
	default:
	}
}

// SetSpeed implements server.SpeedController.
func (p *Player) SetSpeed(v float64) {
	if v <= 0 {
		return
	}
	p.mu.Lock()
	p.speed = v
	p.mu.Unlock()
}

func (p *Player) speedNow() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.speed
}

func (p *Player) isPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.paused
}

// waitIfPaused blocks while paused, until Step/Resume or ctx cancellation
// (mirrors table.Session's own Pause/Resume/Step contract exactly).
func (p *Player) waitIfPaused(ctx context.Context) error {
	for {
		if !p.isPaused() {
			return nil
		}
		select {
		case <-p.stepCh:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// Run plays the recording into h once, or forever (if Loop), until ctx is
// cancelled or a read/decode error occurs.
func (p *Player) Run(ctx context.Context, h *server.Hub) error {
	for {
		if err := p.playOnce(ctx, h); err != nil {
			return err
		}
		if ctx.Err() != nil {
			return nil
		}
		if !p.loop {
			return nil
		}
	}
}

func (p *Player) playOnce(ctx context.Context, h *server.Hub) error {
	f, err := os.Open(p.path)
	if err != nil {
		return fmt.Errorf("replay: open %s: %w", p.path, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)

	var prevTS time.Time
	havePrevTS := false

	for scanner.Scan() {
		if ctx.Err() != nil {
			return nil
		}

		var env protocol.Envelope
		if err := json.Unmarshal(scanner.Bytes(), &env); err != nil {
			return fmt.Errorf("replay: decode %s: %w", p.path, err)
		}

		ts, tsErr := protocol.ParseTS(env.TS)
		if havePrevTS && tsErr == nil {
			if err := p.sleepGap(ctx, ts.Sub(prevTS)); err != nil {
				return nil
			}
		}

		if err := p.waitIfPaused(ctx); err != nil {
			return nil
		}

		h.EmitEnvelope(env)

		if tsErr == nil {
			prevTS = ts
			havePrevTS = true
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("replay: read %s: %w", p.path, err)
	}
	return nil
}

// sleepGap sleeps gap/speed, capped at maxGap/speed, interruptible by ctx.
func (p *Player) sleepGap(ctx context.Context, gap time.Duration) error {
	if gap <= 0 {
		return nil
	}
	speed := p.speedNow()
	d := time.Duration(float64(gap) / speed)
	if cap := time.Duration(float64(maxGap) / speed); d > cap {
		d = cap
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
