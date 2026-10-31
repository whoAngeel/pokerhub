package server

import (
	"context"
	"log"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/whoAngeel/pokerhub/internal/protocol"
)

// serveConn drives one accepted WebSocket connection end to end: registers
// it with the hub, streams the initial payload (snapshot or reconnect
// catch-up) plus the live feed out, and reads/dispatches client messages
// (hello/control/action) concurrently. It returns once the connection is
// done (client disconnect, write failure, or ctx cancellation), having
// already closed c.
//
// c.Write is documented safe for concurrent use by multiple goroutines
// (only Read/Reader isn't), so the hello-triggered replay write in the read
// loop and the live-feed writes in the write loop may interleave but never
// race.
func serveConn(ctx context.Context, c *websocket.Conn, hub *Hub, ctrl Controller, role Role) {
	sub, initial := hub.Connect(role, nil, DefaultQueueSize)
	defer sub.Close()

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	writeDone := make(chan error, 1)
	go func() { writeDone <- writePump(connCtx, c, sub, initial) }()

	readErr := readPump(connCtx, c, hub, ctrl, role, sub)
	cancel() // unblocks writePump's ctx.Done() case

	writeErr := <-writeDone

	code, reason := websocket.StatusNormalClosure, ""
	if readErr != nil || writeErr != nil {
		code, reason = websocket.StatusInternalError, "connection error"
	}
	_ = c.Close(code, reason)
}

// writePump sends initial, then streams sub's live feed until ctx is done,
// the subscription is force-dropped (slow reader), or a write fails.
func writePump(ctx context.Context, c *websocket.Conn, sub *Subscription, initial []protocol.Envelope) error {
	for _, env := range initial {
		if err := wsjson.Write(ctx, c, env); err != nil {
			return err
		}
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-sub.Dropped():
			return errSlowReader
		case env, ok := <-sub.Envelopes():
			if !ok {
				return nil
			}
			if err := wsjson.Write(ctx, c, env); err != nil {
				return err
			}
		}
	}
}

var errSlowReader = errDropped{}

type errDropped struct{}

func (errDropped) Error() string { return "server: connection dropped: slow reader" }

// readPump reads client messages until ctx is done or the read fails
// (including a normal client-initiated close, which also surfaces as an
// error here -- the caller doesn't distinguish, it just stops).
func readPump(ctx context.Context, c *websocket.Conn, hub *Hub, ctrl Controller, role Role, sub *Subscription) error {
	for {
		var env protocol.Envelope
		if err := wsjson.Read(ctx, c, &env); err != nil {
			return err
		}
		switch env.Type {
		case protocol.TypeHello:
			handleHello(ctx, c, hub, role, env)
		case protocol.TypeControl:
			handleControl(ctx, c, ctrl, role, env)
		case protocol.TypeAction:
			writeError(ctx, c, env.HandID, "not supported yet")
		default:
			log.Printf("server: ws: ignoring unknown client message type %q", env.Type)
		}
	}
}

func handleHello(ctx context.Context, c *websocket.Conn, hub *Hub, role Role, env protocol.Envelope) {
	var p protocol.HelloPayload
	if err := env.Decode(&p); err != nil {
		log.Printf("server: ws: bad hello payload: %v", err)
		return
	}
	if p.LastSeq == nil {
		return
	}
	for _, out := range hub.Replay(role, *p.LastSeq) {
		if err := wsjson.Write(ctx, c, out); err != nil {
			return
		}
	}
}

// handleControl dispatches control{pause|resume|step|speed}. Only a
// spectator may send it (PROTOCOL.md §5); a player's attempt is ignored
// with a log line rather than silently accepted. speed against a
// Controller that doesn't implement SpeedController is logged and ignored
// (task spec: "else ignore with log"), never an error reply -- it's a
// best-effort pacing hint, not a required capability.
func handleControl(ctx context.Context, c *websocket.Conn, ctrl Controller, role Role, env protocol.Envelope) {
	if !role.IsSpectator() {
		log.Printf("server: ws: control from non-spectator role %s ignored", role)
		return
	}
	var p protocol.ControlPayload
	if err := env.Decode(&p); err != nil {
		log.Printf("server: ws: bad control payload: %v", err)
		return
	}
	switch p.Cmd {
	case "pause":
		ctrl.Pause()
	case "resume":
		ctrl.Resume()
	case "step":
		ctrl.Step()
	case "speed":
		sc, ok := ctrl.(SpeedController)
		if !ok {
			log.Printf("server: ws: control{speed} not supported by this controller, ignoring")
			return
		}
		if p.Value == nil {
			log.Printf("server: ws: control{speed} missing value, ignoring")
			return
		}
		sc.SetSpeed(*p.Value)
	default:
		writeError(ctx, c, env.HandID, "unknown control cmd")
	}
}

func writeError(ctx context.Context, c *websocket.Conn, handID *int, message string) {
	env, err := protocol.NewEnvelope(protocol.TypeError, handID, protocol.ErrorPayload{Message: message})
	if err != nil {
		return
	}
	_ = wsjson.Write(ctx, c, env)
}
