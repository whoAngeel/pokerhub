package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

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

	tok := resolveToken(*token)
	endpoint, err := tui.NormalizeURL(*url, tok)
	if err != nil {
		return fmt.Errorf("tui: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Best-effort: seed names/persona/model from GET /api/config so the
	// TUI doesn't show "seat N" placeholders for the first few seconds
	// before a "snapshot"/"hand_start" arrives over the WebSocket
	// (docs/TUI_PLAN.md T2 "Names everywhere"). A failure here (server
	// without /api/config, offline, wrong token) is not fatal -- the wire
	// stream fills names in on its own either way.
	fetchCtx, cancelFetch := context.WithTimeout(ctx, 3*time.Second)
	seeds, _ := tui.FetchConfig(fetchCtx, *url, tok)
	cancelFetch()

	model := tui.NewModel(*ascii, seeds)
	program := tea.NewProgram(model, tea.WithContext(ctx))

	client := tui.NewClient(endpoint, program)
	// SetClientMsg hands the Model a way to send control{pause/resume/
	// step/speed} back over this same connection (docs/TUI_PLAN.md T2
	// "Controls"). Sent before client.Run starts; tea.Program's message
	// queue accepts Sends before Run() the same way the pre-existing
	// ConnStateMsg{"connecting"} send below already relied on.
	program.Send(tui.SetClientMsg{Client: client})

	clientCtx, cancelClient := context.WithCancel(ctx)
	defer cancelClient()
	go client.Run(clientCtx)

	_, err = program.Run()
	cancelClient()
	return err
}
