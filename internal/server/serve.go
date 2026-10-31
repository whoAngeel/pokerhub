package server

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/whoAngeel/pokerhub/internal/config"
	"github.com/whoAngeel/pokerhub/internal/table"
)

// Run starts the Echo/WebSocket server bound to addr and blocks until ctx
// is cancelled (then shuts the HTTP server down gracefully, bounded to 5s)
// or ListenAndServe itself fails (e.g. the address is already in use).
//
// h must already be the Sink (or part of a table.MultiSink) that whatever
// table.Session is live was constructed with: Run only serves the HTTP/WS
// side, it never touches the game loop. See Serve for the usual pairing.
func Run(ctx context.Context, addr string, h *Hub, ctrl Controller, pub PublicConfig, debug bool, token string) error {
	app := NewApp(h, ctrl, pub, debug, token)
	srv := &http.Server{Addr: addr, Handler: app}

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server: listen %s: %w", addr, err)
		}
		return nil
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("server: shutdown: %w", err)
		}
		<-errCh // wait for ListenAndServe's goroutine to actually return
		return nil
	}
}

// Serve is the all-in-one entrypoint cmd/pokerd's `serve` subcommand wires
// up: it runs the HTTP/WebSocket server AND sess.Run(ctx, 0) (play hands
// until ctx is cancelled -- see table.Session.Run's hands<=0 case)
// concurrently, and returns once BOTH have stopped, whichever stops first
// causing the other to wind down.
//
// IMPORTANT, and the one place this deviates from a flat
// Serve(ctx, cfg, sess, recordPath) signature: table.Session's sink is
// fixed at construction (no setter), so h must already be the sink --
// typically table.MultiSink{h, recorder} -- sess itself was built with.
// Serve can't retrofit itself as sess's sink after the fact, so the Hub
// (and, if recording, a *replay.Recorder) have to be built BEFORE
// table.NewSession, not inside Serve. Suggested cmd/pokerd wiring:
//
//	pub, err := server.BuildPublicConfig(cfg)
//	hub := server.NewHub(pub.TableStateSeats(), cfg.Table.DecisionTimeoutMS, 0)
//	var sink table.Sink = hub
//	var rec *replay.Recorder
//	if recordPath != "" {
//	    rec, err = replay.NewRecorder(recordPath)
//	    sink = table.MultiSink{hub, rec}
//	}
//	players, memAgents, err := config.Build(cfg)
//	sess, err := table.NewSession(tableCfg, players, sink)
//	sess.SetMemoryAgents(memAgents)
//	err = server.Serve(ctx, cfg, sess, hub, token)
//	if rec != nil { rec.Close() }
//
// (The ideal fix, noted in this task's report for whoever next touches
// internal/table, is a Session.AddSink(Sink) or an equivalent constructor
// option, so Serve could own building the Hub+Recorder end to end exactly
// as first specified.)
func Serve(ctx context.Context, cfg *config.Config, sess *table.Session, h *Hub, token string) error {
	pub, err := BuildPublicConfig(cfg)
	if err != nil {
		return fmt.Errorf("server: serve: %w", err)
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	httpErrCh := make(chan error, 1)
	go func() {
		httpErrCh <- Run(runCtx, cfg.Server.Addr, h, sess, pub, cfg.Server.Debug, token)
		cancel() // the HTTP server stopping (even cleanly) also winds the session down
	}()

	sessErr := sess.Run(runCtx, 0)
	cancel() // the session stopping also winds the HTTP server down
	httpErr := <-httpErrCh

	if sessErr != nil {
		return fmt.Errorf("server: session: %w", sessErr)
	}
	return httpErr
}
