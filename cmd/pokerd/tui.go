package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/whoAngeel/pokerhub/internal/tui"
)

// runTui is `pokerd tui`: a read-only spectator client against a running
// `pokerd serve` or `pokerd replay` (same WebSocket contract either way,
// docs/PROTOCOL.md §7). --url accepts ws://, wss://, http:// or https://
// (see tui.NormalizeURL) so a Cloudflare quick tunnel's https:// URL
// (docs/DEPLOY.md "Túnel de Cloudflare") can be pasted as-is.
func runTui(args []string) error {
	fs := flag.NewFlagSet("tui", flag.ExitOnError)
	url := fs.String("url", "ws://127.0.0.1:8090", "pokerd WebSocket base URL (ws://, wss://, http(s)://)")
	token := fs.String("token", "", "?token= for /ws (default: POKERHUB_TOKEN env/.env)")
	ascii := fs.Bool("ascii", false, "use plain ASCII (s h d c, no emoji) instead of suit glyphs")
	if err := fs.Parse(args); err != nil {
		return err
	}

	endpoint, err := tui.NormalizeURL(*url, resolveToken(*token))
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	model := tui.NewModel(*ascii)
	program := tea.NewProgram(model, tea.WithContext(ctx))

	client := tui.NewClient(endpoint, program)
	clientCtx, cancelClient := context.WithCancel(ctx)
	defer cancelClient()
	go client.Run(clientCtx)

	_, err = program.Run()
	cancelClient()
	return err
}
