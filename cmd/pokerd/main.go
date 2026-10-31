// Command pokerd is the single binary for the poker table: `sim` (bots vs
// bots in the terminal), `serve` (WebSocket server for the frontend, see
// docs/PROTOCOL.md) and `replay` (serve a recorded .jsonl without a GPU).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/whoAngeel/pokerhub/internal/config"
	"github.com/whoAngeel/pokerhub/internal/replay"
	"github.com/whoAngeel/pokerhub/internal/server"
	"github.com/whoAngeel/pokerhub/internal/table"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "sim":
		err = runSim(os.Args[2:])
	case "serve":
		err = runServe(os.Args[2:])
	case "replay":
		err = runReplay(os.Args[2:])
	default:
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "pokerd:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  pokerd sim    [--config f] [--hands N] [--seed N] [--fast] [--mock] [--record f.jsonl]
  pokerd serve  [--config f] [--addr host:port] [--seed N] [--fast] [--mock] [--record f.jsonl]
  pokerd replay [--config f] [--addr host:port] [--speed X] [--loop] <file.jsonl>`)
}

// sessionFlags are shared by sim and serve.
type sessionFlags struct {
	configPath *string
	seed       *int64
	fast       *bool
	mock       *bool
	record     *string
}

func addSessionFlags(fs *flag.FlagSet) sessionFlags {
	return sessionFlags{
		configPath: fs.String("config", "configs/config.yaml", "path to config.yaml"),
		seed:       fs.Int64("seed", 0, "RNG seed override (0 = use config, or time-based if config seed is also 0)"),
		fast:       fs.Bool("fast", false, "disable pacing between actions (action_delay_ms = 0)"),
		mock:       fs.Bool("mock", false, "replace every provider with the deterministic mock provider (no network, no GPU)"),
		record:     fs.String("record", "", "also record the god-view event stream to this .jsonl file"),
	}
}

// buildSession loads the config, builds the players and a Session that
// emits to sink (plus a Recorder when --record is set). The returned close
// func flushes the recording.
func buildSession(f sessionFlags, sink table.Sink) (*config.Config, *table.Session, func(), error) {
	cfg, err := config.Load(*f.configPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load config: %w", err)
	}

	var buildOpts []config.BuildOption
	if *f.mock {
		buildOpts = append(buildOpts, config.WithMockProviders())
	}
	players, memAgents, err := config.Build(cfg, buildOpts...)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("build players: %w", err)
	}

	tcfg := table.Config{
		SmallBlind:        cfg.Table.SmallBlind,
		BigBlind:          cfg.Table.BigBlind,
		BuyIn:             cfg.Table.BuyIn,
		Seed:              cfg.Table.Seed,
		ActionDelayMS:     cfg.Table.ActionDelayMS,
		DecisionTimeoutMS: cfg.Table.DecisionTimeoutMS,
		EquityIters:       cfg.Table.EquityIters,
	}
	if *f.seed != 0 {
		tcfg.Seed = *f.seed
	}
	if *f.fast {
		tcfg.ActionDelayMS = 0
	}

	closeFn := func() {}
	if *f.record != "" {
		rec, err := replay.NewRecorder(*f.record, cfg.Table.DecisionTimeoutMS)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("recorder: %w", err)
		}
		sink = table.MultiSink{sink, rec}
		closeFn = func() { _ = rec.Close() }
	}

	sess, err := table.NewSession(tcfg, players, sink)
	if err != nil {
		closeFn()
		return nil, nil, nil, fmt.Errorf("new session: %w", err)
	}
	sess.SetMemoryAgents(memAgents)
	return cfg, sess, closeFn, nil
}

func runSim(args []string) error {
	fs := flag.NewFlagSet("sim", flag.ExitOnError)
	f := addSessionFlags(fs)
	hands := fs.Int("hands", 5, "number of hands to play")
	if err := fs.Parse(args); err != nil {
		return err
	}

	printer := table.NewPrinter(os.Stdout, table.IsTerminal(os.Stdout))
	_, sess, closeRec, err := buildSession(f, printer)
	if err != nil {
		return err
	}
	defer closeRec()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := sess.Run(ctx, *hands); err != nil {
		return fmt.Errorf("run: %w", err)
	}

	printer.PrintLatencySummary()
	return nil
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	f := addSessionFlags(fs)
	addr := fs.String("addr", "", "listen address (default: server.addr from config)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	// The Hub must exist before the Session (it is the Session's sink), and
	// building it needs the public seat list, so load the config once here.
	cfg, err := config.Load(*f.configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if *addr != "" {
		cfg.Server.Addr = *addr
	}
	pub, err := server.BuildPublicConfig(cfg)
	if err != nil {
		return fmt.Errorf("public config: %w", err)
	}
	hub := server.NewHub(pub.TableStateSeats(), cfg.Table.DecisionTimeoutMS, 0)

	_, sess, closeRec, err := buildSession(f, hub)
	if err != nil {
		return err
	}
	defer closeRec()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "pokerd: serving on http://%s  (ws: /ws?role=spectator | /ws?role=player&seat=N)\n", cfg.Server.Addr)
	return server.Serve(ctx, cfg, sess, hub)
}

func runReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	configPath := fs.String("config", "configs/config.yaml", "path to config.yaml (server addr, public seats)")
	addr := fs.String("addr", "", "listen address (default: server.addr from config)")
	speed := fs.Float64("speed", 1, "playback speed multiplier")
	loop := fs.Bool("loop", false, "restart the recording when it ends")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("replay: expected exactly one .jsonl file argument")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if *addr != "" {
		cfg.Server.Addr = *addr
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "pokerd: replaying %s on http://%s (speed %.1fx)\n", fs.Arg(0), cfg.Server.Addr, *speed)
	return replay.Serve(ctx, cfg, fs.Arg(0), *speed, *loop)
}
