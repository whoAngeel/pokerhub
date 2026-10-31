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
	"strings"
	"syscall"

	"github.com/whoAngeel/pokerhub/internal/config"
	"github.com/whoAngeel/pokerhub/internal/replay"
	"github.com/whoAngeel/pokerhub/internal/server"
	"github.com/whoAngeel/pokerhub/internal/table"
)

func main() {
	args, envFile := splitEnvFileFlag(os.Args[1:])
	loadDotEnv(envFile) // never overrides a var already set (e.g. real env beats .env)

	if len(args) < 1 {
		usage()
		os.Exit(1)
	}

	var err error
	switch args[0] {
	case "sim":
		err = runSim(args[1:])
	case "serve":
		err = runServe(args[1:])
	case "replay":
		err = runReplay(args[1:])
	case "tui":
		err = runTui(args[1:])
	default:
		usage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "pokerd:", err)
		os.Exit(1)
	}
}

// splitEnvFileFlag pulls a leading global "--env-file <path>" or
// "--env-file=<path>" off args (it comes before the subcommand, e.g.
// `pokerd --env-file ~/pokerhub.env tui --url ...`) and returns the
// remaining args plus the env file path to load (".env" if not given).
// This has to happen before any subcommand's own flag.Parse, and before
// loadDotEnv, since --token flags below resolve from the environment it
// populates.
func splitEnvFileFlag(args []string) ([]string, string) {
	if len(args) == 0 {
		return args, ".env"
	}
	if args[0] == "--env-file" && len(args) >= 2 {
		return args[2:], args[1]
	}
	if v, ok := strings.CutPrefix(args[0], "--env-file="); ok {
		return args[1:], v
	}
	return args, ".env"
}

// resolveToken returns the auth token to use: flagVal (from a subcommand's
// own --token) if set, else POKERHUB_TOKEN from the environment (populated
// by loadDotEnv from --env-file/.env, or already set in the real
// environment). Never logged -- callers must not fmt.Print this value.
func resolveToken(flagVal string) string {
	if flagVal != "" {
		return flagVal
	}
	return os.Getenv("POKERHUB_TOKEN")
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  pokerd [--env-file path]  sim    [--config f] [--hands N] [--seed N] [--fast] [--mock] [--record f.jsonl]
  pokerd [--env-file path]  serve  [--config f] [--addr host:port] [--seed N] [--fast] [--mock] [--record f.jsonl] [--token T]
  pokerd [--env-file path]  replay [--config f] [--addr host:port] [--speed X] [--loop] [--token T] <file.jsonl>
  pokerd [--env-file path]  tui    [--url ws|wss|http(s)://host:port] [--token T] [--ascii]

--env-file defaults to ".env" in the current directory. --token defaults to
the POKERHUB_TOKEN environment variable (loaded from --env-file) when not
given explicitly.`)
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
	token := fs.String("token", "", "required ?token= for /ws and /api/config (default: POKERHUB_TOKEN env/.env)")
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

	tok := resolveToken(*token)
	fmt.Fprintf(os.Stderr, "pokerd: serving on http://%s  (ws: /ws?role=spectator | /ws?role=player&seat=N; token required: %v)\n", cfg.Server.Addr, tok != "")
	return server.Serve(ctx, cfg, sess, hub, tok)
}

func runReplay(args []string) error {
	fs := flag.NewFlagSet("replay", flag.ExitOnError)
	configPath := fs.String("config", "configs/config.yaml", "path to config.yaml (server addr, public seats)")
	addr := fs.String("addr", "", "listen address (default: server.addr from config)")
	speed := fs.Float64("speed", 1, "playback speed multiplier")
	loop := fs.Bool("loop", false, "restart the recording when it ends")
	token := fs.String("token", "", "required ?token= for /ws and /api/config (default: POKERHUB_TOKEN env/.env)")
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

	tok := resolveToken(*token)
	fmt.Fprintf(os.Stderr, "pokerd: replaying %s on http://%s (speed %.1fx; token required: %v)\n", fs.Arg(0), cfg.Server.Addr, *speed, tok != "")
	return replay.Serve(ctx, cfg, fs.Arg(0), *speed, *loop, tok)
}
