// Package tui is a read-only spectator client for the pokerd WebSocket
// contract (docs/PROTOCOL.md), built with Bubble Tea v2
// (charm.land/bubbletea/v2). It is just one more client of the protocol --
// same as the React frontend in client/ -- and reuses internal/protocol's
// Envelope/TableState wholesale instead of re-deriving table state from the
// wire events itself (docs/TUI_PLAN.md).
//
// This file (client.go) owns the WebSocket connection lifecycle: dialing,
// normalizing whatever URL form the user gave into the /ws endpoint,
// reading envelopes and forwarding them into the Bubble Tea program as
// messages, and reconnecting with backoff (sending hello{last_seq} once
// back online, per PROTOCOL.md §1 "Reconexión").
package tui

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/whoAngeel/pokerhub/internal/protocol"
)

// EnvMsg wraps one Envelope received from the server as a Bubble Tea
// message; Update folds it into the Model via protocol.TableState.Apply.
type EnvMsg struct{ Env protocol.Envelope }

// ConnStateMsg reports a WebSocket lifecycle transition so the header/log
// can show "conectando…", "en vivo" or "desconectado, reintentando…".
type ConnStateMsg struct {
	State string // "connecting" | "connected" | "disconnected"
	Err   error
}

// NormalizeURL turns whatever base the user passed (ws://, wss://,
// http(s)://, or a bare "host:port") into the full spectator WebSocket
// endpoint: <scheme>://<host>/ws?role=spectator[&token=...]. http/https are
// accepted and mapped to ws/wss since that's the natural way to paste a
// Cloudflare quick-tunnel URL (cloudflared prints an https:// URL, but the
// tunnel also carries wss:// upgrades on the same hostname).
func NormalizeURL(base, token string) (string, error) {
	if base == "" {
		return "", fmt.Errorf("tui: empty url")
	}
	if !strings.Contains(base, "://") {
		base = "ws://" + base
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("tui: bad url %q: %w", base, err)
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	case "ws", "wss":
		// already fine
	default:
		return "", fmt.Errorf("tui: unsupported scheme %q (want ws/wss/http/https)", u.Scheme)
	}
	u.Path = "/ws"
	q := url.Values{}
	q.Set("role", "spectator")
	if token != "" {
		q.Set("token", token)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Client drives one logical (reconnecting) WebSocket session against
// NormalizeURL's endpoint and feeds everything it sees into program via
// Send. It holds no game state itself -- that's the Model's job via
// EnvMsg -- only the connection and the last_seq needed to resume.
type Client struct {
	url     string
	program *tea.Program
	lastSeq int64
}

// NewClient builds a Client for endpoint url (already normalized), sending
// messages into program.
func NewClient(url string, program *tea.Program) *Client {
	return &Client{url: url, program: program}
}

// Run dials and streams until ctx is cancelled, retrying with capped
// exponential backoff on any disconnect or dial failure. Safe to run in its
// own goroutine; it never touches program's Model directly, only
// program.Send.
func (c *Client) Run(ctx context.Context) {
	backoff := time.Second
	const maxBackoff = 10 * time.Second
	for ctx.Err() == nil {
		c.program.Send(ConnStateMsg{State: "connecting"})
		err := c.connectOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		c.program.Send(ConnStateMsg{State: "disconnected", Err: err})

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// connectOnce performs one dial + read loop, returning when the connection
// drops (any error, including a clean server close) or ctx is done.
func (c *Client) connectOnce(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, c.url, nil)
	if err != nil {
		return fmt.Errorf("tui: dial: %w", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if c.lastSeq > 0 {
		hello, err := protocol.NewEnvelope(protocol.TypeHello, nil, protocol.HelloPayload{
			Role:    "spectator",
			LastSeq: protocol.Int64Ptr(c.lastSeq),
		})
		if err == nil {
			_ = wsjson.Write(ctx, conn, hello) // best-effort: a lost hello just means a fresh snapshot anyway
		}
	}

	c.program.Send(ConnStateMsg{State: "connected"})

	for {
		var env protocol.Envelope
		if err := wsjson.Read(ctx, conn, &env); err != nil {
			return err
		}
		if env.Seq > c.lastSeq {
			c.lastSeq = env.Seq
		}
		c.program.Send(EnvMsg{Env: env})
	}
}
