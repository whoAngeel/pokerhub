package replay

import (
	"context"
	"fmt"

	"github.com/whoAngeel/pokerhub/internal/config"
	"github.com/whoAngeel/pokerhub/internal/server"
)

// Serve starts the exact same Echo/WebSocket server a live `pokerd serve`
// would (GET /healthz, /api/config, /ws -- same server.Project filtering,
// same Hub/ring-buffer/reconnect behavior), fed by file's recording instead
// of a live table.Session. cfg's table/seat composition builds the public
// config and seeds the Hub's TableState exactly as a live run would;
// cfg.Server.{Addr,Debug} and cfg.Table.DecisionTimeoutMS are reused the
// same way too, so a replay is indistinguishable from the original run at
// the WebSocket contract level (docs/PROTOCOL.md §7).
//
// speed<=0 means real time (1x); loop restarts the file from the top once
// it ends, instead of stopping. control{pause|resume|step|speed} from a
// spectator connection works against the replay (Player satisfies
// server.Controller and server.SpeedController).
func Serve(ctx context.Context, cfg *config.Config, file string, speed float64, loop bool) error {
	pub, err := server.BuildPublicConfig(cfg)
	if err != nil {
		return fmt.Errorf("replay: serve: %w", err)
	}
	hub := server.NewHub(pub.TableStateSeats(), cfg.Table.DecisionTimeoutMS, 0)
	player := NewPlayer(file, speed, loop)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	httpErrCh := make(chan error, 1)
	go func() {
		httpErrCh <- server.Run(runCtx, cfg.Server.Addr, hub, player, pub, cfg.Server.Debug)
		cancel()
	}()

	playErr := player.Run(runCtx, hub)
	cancel()
	httpErr := <-httpErrCh

	if playErr != nil {
		return fmt.Errorf("replay: player: %w", playErr)
	}
	return httpErr
}
