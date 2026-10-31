// Command pokerd is the single binary for the poker table: `sim` (bots vs
// bots in the terminal, PLAN F2), and stubs for `serve` and `replay` (PLAN
// F3, left for a later phase).
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/whoAngeel/pokerhub/internal/config"
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
		fmt.Println("serve: not implemented yet")
	case "replay":
		fmt.Println("replay: not implemented yet")
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
	fmt.Fprintln(os.Stderr, "usage: pokerd <sim|serve|replay> [flags]")
}

func runSim(args []string) error {
	fs := flag.NewFlagSet("sim", flag.ExitOnError)
	configPath := fs.String("config", "configs/config.yaml", "path to config.yaml")
	hands := fs.Int("hands", 5, "number of hands to play")
	seed := fs.Int64("seed", 0, "RNG seed override (0 = use config, or time-based if config seed is also 0)")
	fast := fs.Bool("fast", false, "disable pacing between actions (action_delay_ms = 0)")
	mock := fs.Bool("mock", false, "replace every provider with the deterministic mock provider (no network, no GPU)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	var buildOpts []config.BuildOption
	if *mock {
		buildOpts = append(buildOpts, config.WithMockProviders())
	}
	players, err := config.Build(cfg, buildOpts...)
	if err != nil {
		return fmt.Errorf("build players: %w", err)
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
	if *seed != 0 {
		tcfg.Seed = *seed
	}
	if *fast {
		tcfg.ActionDelayMS = 0
	}

	printer := table.NewPrinter(os.Stdout, table.IsTerminal(os.Stdout))

	sess, err := table.NewSession(tcfg, players, printer)
	if err != nil {
		return fmt.Errorf("new session: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := sess.Run(ctx, *hands); err != nil {
		return fmt.Errorf("run: %w", err)
	}

	printer.PrintLatencySummary()
	return nil
}
