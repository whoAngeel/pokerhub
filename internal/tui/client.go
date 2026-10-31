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
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
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

// SetClientMsg hands the live *Client to the Model so key presses (T2:
// pause/resume/step/speed) can send a "control" message back over the same
// WebSocket connection Client.Run is driving. It travels as an ordinary
// tea.Msg (via program.Send right after NewClient, before client.Run
// starts) rather than a constructor field, because the Model has to exist
// before tea.NewProgram(model) can hand out the *tea.Program a Client
// needs -- see cmd/pokerd/tui.go.
type SetClientMsg struct{ Client *Client }

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

	// mu guards conn/connCtx, set for the lifetime of one live connection by
	// connectOnce and cleared on disconnect, so SendControl (called from the
	// Bubble Tea Update goroutine on a keypress) never races with
	// connectOnce/Run (the connection goroutine) over which connection is
	// "current".
	mu      sync.Mutex
	conn    *websocket.Conn
	connCtx context.Context
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

	c.mu.Lock()
	c.conn, c.connCtx = conn, ctx
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn, c.connCtx = nil, nil
		c.mu.Unlock()
	}()

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

// SendControl writes a "control" message (PROTOCOL.md §5: only a spectator
// may send one) over whichever connection is currently live, for the
// pause/resume/step/speed keys (docs/TUI_PLAN.md T2). Returns an error --
// surfaced by the Model as a footer flash -- when there's no live
// connection to write to right now; the caller is expected to retry on the
// next keypress once reconnected rather than queue anything.
func (c *Client) SendControl(cmd string, value *float64) error {
	c.mu.Lock()
	conn, ctx := c.conn, c.connCtx
	c.mu.Unlock()
	if conn == nil {
		return fmt.Errorf("tui: not connected")
	}
	env, err := protocol.NewEnvelope(protocol.TypeControl, nil, protocol.ControlPayload{Cmd: cmd, Value: value})
	if err != nil {
		return err
	}
	return wsjson.Write(ctx, conn, env)
}

// httpConfigURL mirrors NormalizeURL but targets the plain HTTP
// "/api/config" endpoint (PROTOCOL.md §1) instead of the WebSocket "/ws"
// one: ws/wss map to http/https, same host, same ?token=.
func httpConfigURL(base, token string) (string, error) {
	if base == "" {
		return "", fmt.Errorf("tui: empty url")
	}
	if !strings.Contains(base, "://") {
		base = "http://" + base
	}
	u, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("tui: bad url %q: %w", base, err)
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	case "http", "https":
		// already fine
	default:
		return "", fmt.Errorf("tui: unsupported scheme %q (want ws/wss/http/https)", u.Scheme)
	}
	u.Path = "/api/config"
	if token != "" {
		q := url.Values{}
		q.Set("token", token)
		u.RawQuery = q.Encode()
	} else {
		u.RawQuery = ""
	}
	return u.String(), nil
}

// configSeat/configResp decode GET /api/config's body (server.PublicConfig,
// internal/server/config.go) without internal/tui importing internal/server
// -- this package only needs the handful of fields NewTableState wants to
// seed, not the HTTP app itself.
type configSeat struct {
	Seat      int     `json:"seat"`
	Name      string  `json:"name"`
	PersonaID *string `json:"persona_id"`
	Kind      string  `json:"kind"`
	Model     *string `json:"model"`
}

type configResp struct {
	BuyIn int          `json:"buy_in"`
	Seats []configSeat `json:"seats"`
}

// FetchConfig performs one synchronous GET against base's "/api/config"
// (PROTOCOL.md §1) and renders the result as the []protocol.Seat
// NewTableState wants, so `pokerd tui` can show real names/personas/models
// from the moment it starts instead of "seat N" placeholders for the few
// seconds before the first "snapshot"/"hand_start" arrives over the
// WebSocket (docs/TUI_PLAN.md T2 "Names everywhere"). Best-effort by
// design: callers should treat any error as "no seed data yet, the wire
// stream will fill names in on its own" rather than fatal.
func FetchConfig(ctx context.Context, base, token string) ([]protocol.Seat, error) {
	endpoint, err := httpConfigURL(base, token)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tui: GET /api/config: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tui: GET /api/config: status %d", resp.StatusCode)
	}
	var cfg configResp
	if err := json.NewDecoder(resp.Body).Decode(&cfg); err != nil {
		return nil, fmt.Errorf("tui: decode /api/config: %w", err)
	}
	seats := make([]protocol.Seat, len(cfg.Seats))
	for i, s := range cfg.Seats {
		seats[i] = protocol.Seat{
			Seat: s.Seat, Name: s.Name, PersonaID: s.PersonaID, Kind: s.Kind,
			Stack: cfg.BuyIn, Status: "active", Model: s.Model,
		}
	}
	return seats, nil
}
