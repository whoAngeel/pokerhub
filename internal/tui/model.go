package tui

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/whoAngeel/pokerhub/internal/protocol"
)

// logCap is the size of the ring buffer of recent action/street/showdown/pot
// lines (docs/TUI_PLAN.md T1/T2: the bottom log is deliberately narrowed to
// those four event families in T2 -- chat/reasoning/memory_update moved to
// the side panel's tabs below).
const logCap = 200

// chatCap/memoryCap/agentErrCap/reasonCap bound the side panel's ring
// buffers (docs/TUI_PLAN.md T2: "últimos N razonamientos" etc.).
const (
	chatCap     = 200
	memoryCap   = 200
	agentErrCap = 20
	reasonCap   = 8
)

// sideTab names the three side-panel tabs (docs/TUI_PLAN.md T2).
type sideTab int

const (
	tabChat sideTab = iota
	tabReasoning
	tabMemory
)

func (t sideTab) String() string {
	switch t {
	case tabChat:
		return "Chat"
	case tabReasoning:
		return "Razonamiento"
	case tabMemory:
		return "Memoria"
	default:
		return "?"
	}
}

// reasoningEntry is one spectator-only "reasoning" event, kept structured
// (rather than pre-rendered) so renderReasoningTab can re-wrap it to
// whatever panel width the terminal currently has.
type reasoningEntry struct {
	seatName  string
	hand      string
	odds      string
	reads     string
	plan      string
	latencyMS int64
	fallback  bool
	warnings  []string
}

// Model is the Bubble Tea root model for `pokerd tui`. It keeps
// protocol.TableState (the god-view reducer this package REUSES rather
// than re-deriving -- see internal/protocol/state.go) plus the UI-only
// state TableState doesn't track: the bottom log, the side panel's three
// tab buffers, pacing-control display state, winner highlight, help and
// connection status.
type Model struct {
	ts *protocol.TableState

	log       []string
	thinking  *int // seat currently thinking, nil if nobody
	connState string
	connErr   error

	width, height int
	ascii         bool
	quitting      bool

	spin spinner.Model

	// Side panel (docs/TUI_PLAN.md T2 "Side panel with tabs").
	activeTab sideTab
	showPanel bool // user toggle for the narrow (<150 cols) layout; always true when wide
	panelW    int  // last-laid-out inner content width, used when wrapping new entries
	chatLines []string
	reasonBuf []reasoningEntry
	agentErrs []string
	memoLines []string
	chatVP    viewport.Model
	reasonVP  viewport.Model
	memoryVP  viewport.Model

	// Controls (docs/TUI_PLAN.md T2 "Controls").
	client         *Client
	paused         bool
	speed          float64
	footerErrMsg   string
	footerErrUntil time.Time

	// Winner highlight at showdown/pot_awarded (docs/TUI_PLAN.md T2
	// "Highlight winner"): seat -> amount won, valid while time.Now() is
	// before winnerUntil.
	winnerSeats map[int]int
	winnerUntil time.Time

	keys keyMap
	help help.Model
}

// NewModel builds a Model ready to Run. seeds, when non-empty, pre-seeds
// TableState from GET /api/config (tui.FetchConfig) so names/persona/model
// render correctly from the first frame instead of "seat N" placeholders
// until the first "snapshot"/"hand_start" arrives over the WebSocket
// (docs/TUI_PLAN.md T2 "Names everywhere"). Pass nil when no config fetch
// was attempted or it failed -- ts stays nil and seeds itself from the
// first Envelope, exactly as T1 did.
func NewModel(ascii bool, seeds []protocol.Seat) Model {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	if ascii {
		sp = spinner.New(spinner.WithSpinner(spinner.Line))
	}
	var ts *protocol.TableState
	if len(seeds) > 0 {
		ts = protocol.NewTableState(seeds)
	}
	m := Model{
		ts:        ts,
		connState: "connecting",
		ascii:     ascii,
		width:     110,
		height:    32,
		spin:      sp,
		speed:     1,
		panelW:    36,
		chatVP:    newPanelViewport(36, 8),
		reasonVP:  newPanelViewport(36, 8),
		memoryVP:  newPanelViewport(36, 8),
		keys:      newKeyMap(),
		help:      help.New(),
	}
	m.layoutPanels()
	return m
}

func newPanelViewport(w, h int) viewport.Model {
	vp := viewport.New(viewport.WithWidth(w), viewport.WithHeight(h))
	vp.SoftWrap = false // content arrives already wrapped to the panel width (see wrapLines)
	return vp
}

// Init starts the spinner ticking (harmless even while nobody is thinking;
// View only renders it next to the seat Model.thinking names). Its
// recurring tick also doubles as the repaint heartbeat that expires the
// winner highlight and footer error flash after ~3s without needing a
// dedicated timer message.
func (m Model) Init() tea.Cmd {
	return m.spin.Tick
}

// Update folds one Bubble Tea message into m. EnvMsg is the only message
// that touches game state, and it does so entirely through
// protocol.TableState.Apply -- Update never inspects a payload's fields
// itself beyond what's needed for the log/panel lines and the thinking cue.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.help.SetWidth(m.width)
		m.layoutPanels()
		return m, nil

	case tea.KeyPressMsg:
		return m.handleKey(msg)

	case SetClientMsg:
		m.client = msg.Client
		return m, nil

	case ConnStateMsg:
		m.connState = msg.State
		m.connErr = msg.Err
		return m, nil

	case EnvMsg:
		m.applyEnvelope(msg.Env)
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

// handleKey dispatches one keypress: quit/help/panel/pacing controls first,
// then anything unmatched falls through to the active tab's viewport (so
// PgUp/PgDn/↑/↓ scroll chat/razonamiento/memoria without a dedicated case
// per key here).
func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		m.quitting = true
		return m, tea.Quit

	case key.Matches(msg, m.keys.Help):
		m.help.ShowAll = !m.help.ShowAll
		return m, nil

	case key.Matches(msg, m.keys.TogglePanel):
		m.showPanel = !m.showPanel
		m.layoutPanels()
		return m, nil

	case key.Matches(msg, m.keys.Tab):
		m.activeTab = (m.activeTab + 1) % 3
		return m, nil
	case key.Matches(msg, m.keys.Tab1):
		m.activeTab = tabChat
		return m, nil
	case key.Matches(msg, m.keys.Tab2):
		m.activeTab = tabReasoning
		return m, nil
	case key.Matches(msg, m.keys.Tab3):
		m.activeTab = tabMemory
		return m, nil

	case key.Matches(msg, m.keys.Pause):
		if m.paused {
			m.sendControl("resume", nil)
			m.paused = false
		} else {
			m.sendControl("pause", nil)
			m.paused = true
		}
		return m, nil

	case key.Matches(msg, m.keys.Step):
		m.sendControl("step", nil)
		return m, nil

	case key.Matches(msg, m.keys.SpeedUp):
		ns := clampSpeed(m.speed * 2)
		m.sendControl("speed", &ns)
		m.speed = ns
		return m, nil

	case key.Matches(msg, m.keys.SpeedDown):
		ns := clampSpeed(m.speed / 2)
		m.sendControl("speed", &ns)
		m.speed = ns
		return m, nil
	}

	var cmd tea.Cmd
	switch m.activeTab {
	case tabChat:
		m.chatVP, cmd = m.chatVP.Update(msg)
	case tabReasoning:
		m.reasonVP, cmd = m.reasonVP.Update(msg)
	case tabMemory:
		m.memoryVP, cmd = m.memoryVP.Update(msg)
	}
	return m, cmd
}

// clampSpeed keeps the "speed" control's value in PROTOCOL.md §5's
// documented range (0.25..4, doubling/halving each keypress).
func clampSpeed(v float64) float64 {
	if v < 0.25 {
		return 0.25
	}
	if v > 4 {
		return 4
	}
	return v
}

// sendControl writes a control{cmd,value} message over the live WS
// connection (via Client.SendControl) and flashes any error in the footer
// (docs/TUI_PLAN.md T2: "Handle the server's error reply by flashing it in
// the footer" -- this also covers the client-side "not connected" case,
// which the server itself never reports but is just as worth surfacing).
func (m *Model) sendControl(cmd string, value *float64) {
	if m.client == nil {
		m.flashFooterError("no conectado todavía")
		return
	}
	if err := m.client.SendControl(cmd, value); err != nil {
		m.flashFooterError(err.Error())
	}
}

func (m *Model) flashFooterError(msg string) {
	m.footerErrMsg = msg
	m.footerErrUntil = time.Now().Add(3 * time.Second)
}

// applyEnvelope is the one place that both feeds env into TableState and
// derives the UI-only bits (log line, panel buffers, thinking cue, winner
// highlight) TableState doesn't carry.
func (m *Model) applyEnvelope(env protocol.Envelope) {
	if m.ts == nil {
		// Normally the very first Envelope a connection ever receives is a
		// snapshot (PROTOCOL.md §2), whose TypeSnapshot case in Apply fully
		// seeds names/kind/etc. from table composition. A raw recording fed
		// straight into Update (as the one test in this package does, and
		// as `pokerd replay` plays back before any client connects) starts
		// from hand_start instead; seatFor's defensive auto-vivify covers
		// that case too, just without persona/kind metadata until a real
		// snapshot (or a live connection to replay's own Hub) supplies it.
		m.ts = protocol.NewTableState(nil)
	}
	if err := m.ts.Apply(env); err != nil {
		m.log = appendLog(m.log, errStyle.Render("error aplicando "+env.Type+": "+err.Error()))
		return
	}

	switch env.Type {
	case protocol.TypeThinking:
		var p protocol.ThinkingPayload
		if env.Decode(&p) == nil {
			seat := p.Seat
			m.thinking = &seat
		}
	case protocol.TypeAction:
		m.thinking = nil
		var p protocol.ActionPayload
		if env.Decode(&p) == nil {
			m.log = appendLog(m.log, actionLine(m.seatName(p.Seat), p))
		}
	case protocol.TypeChat:
		var p protocol.ChatPayload
		if env.Decode(&p) == nil {
			m.chatLines = appendRing(m.chatLines, fmt.Sprintf("💬 %s: %q", m.seatName(p.Seat), p.Say), chatCap)
			m.refreshChat()
		}
	case protocol.TypeHandStart:
		var p protocol.HandStartPayload
		if env.Decode(&p) == nil {
			m.thinking = nil
			m.log = appendLog(m.log, headerStyle.Render(fmt.Sprintf("── mano #%d (ciegas %d/%d) ──", p.HandID, p.SmallBlind, p.BigBlind)))
		}
	case protocol.TypeStreet:
		var p protocol.StreetPayload
		if env.Decode(&p) == nil {
			m.log = appendLog(m.log, fmt.Sprintf("calle: %s  board %s", p.Street, renderCards(p.Board, m.ascii)))
		}
	case protocol.TypeShowdown:
		var p protocol.ShowdownPayload
		if env.Decode(&p) == nil {
			for _, r := range p.Reveals {
				m.log = appendLog(m.log, fmt.Sprintf("showdown: %s muestra %s (%s)", m.seatName(r.Seat), renderCards(r.Cards, m.ascii), r.Rank))
			}
		}
	case protocol.TypePotAwarded:
		var p protocol.PotAwardedPayload
		if env.Decode(&p) == nil {
			winners := make(map[int]int)
			for _, pot := range p.Pots {
				names := make([]string, len(pot.Winners))
				share := pot.Amount
				if len(pot.Winners) > 0 {
					share = pot.Amount / len(pot.Winners)
				}
				for i, w := range pot.Winners {
					names[i] = m.seatName(w)
					winners[w] += share
				}
				m.log = appendLog(m.log, goodStyle.Render(fmt.Sprintf("bote %d → %v", pot.Amount, names)))
			}
			m.winnerSeats = winners
			m.winnerUntil = time.Now().Add(3 * time.Second)
		}
	case protocol.TypeReasoning:
		var p protocol.ReasoningPayload
		if env.Decode(&p) == nil {
			m.reasonBuf = append(m.reasonBuf, reasoningEntry{
				seatName:  m.seatName(p.Seat),
				hand:      p.Hand,
				odds:      p.Odds,
				reads:     p.Reads,
				plan:      p.Plan,
				latencyMS: p.Meta.LatencyMS,
				fallback:  p.Meta.Fallback,
				warnings:  p.Meta.Warnings,
			})
			if len(m.reasonBuf) > reasonCap {
				m.reasonBuf = m.reasonBuf[len(m.reasonBuf)-reasonCap:]
			}
			m.refreshReasoning()
		}
	case protocol.TypeMemoryUpdate:
		var p protocol.MemoryUpdatePayload
		if env.Decode(&p) == nil {
			observer := m.seatName(p.Observer)
			for _, upd := range p.Updates {
				rival := m.seatName(upd.Seat)
				for _, note := range upd.Notes {
					m.memoLines = appendRing(m.memoLines, fmt.Sprintf("%s → %s: %s", observer, rival, note), memoryCap)
				}
			}
			m.refreshMemory()
		}
	case protocol.TypeAgentError:
		var p protocol.AgentErrorPayload
		if env.Decode(&p) == nil {
			line := fmt.Sprintf("⚠ %s: %s", m.seatName(p.Seat), p.Error)
			if p.Fallback {
				line += " [fallback]"
			}
			if len(p.Warnings) > 0 {
				line += " (" + strings.Join(p.Warnings, ", ") + ")"
			}
			m.agentErrs = appendRing(m.agentErrs, line, agentErrCap)
			m.refreshReasoning()
		}
	case protocol.TypeError:
		var p protocol.ErrorPayload
		if env.Decode(&p) == nil {
			m.flashFooterError(p.Message)
		}
	}
}

// seatName looks up a seat's display name from the current snapshot; "?"
// if ts isn't seeded yet or the seat is unknown.
func (m Model) seatName(seat int) string {
	if m.ts == nil {
		return fmt.Sprintf("#%d", seat)
	}
	snap, err := m.ts.Snapshot()
	if err != nil {
		return fmt.Sprintf("#%d", seat)
	}
	var p protocol.SnapshotPayload
	if snap.Decode(&p) != nil {
		return fmt.Sprintf("#%d", seat)
	}
	for _, s := range p.Seats {
		if s.Seat == seat && s.Name != "" {
			return s.Name
		}
	}
	return fmt.Sprintf("#%d", seat)
}

// actionLine renders one "action" event's log line using the seat's real
// name (docs/TUI_PLAN.md T2 "Names everywhere": "El Payaso iguala 10", not
// "seat 4 iguala 10").
func actionLine(name string, p protocol.ActionPayload) string {
	switch p.Type {
	case "fold":
		return fmt.Sprintf("%s se retira", name)
	case "check":
		return fmt.Sprintf("%s pasa", name)
	case "call":
		return fmt.Sprintf("%s iguala %d", name, p.Amount)
	case "bet":
		return fmt.Sprintf("%s apuesta %d", name, p.Amount)
	case "raise":
		return fmt.Sprintf("%s sube %d", name, p.Amount)
	case "all_in":
		return goodStyle.Render(fmt.Sprintf("%s all-in %d", name, p.Amount))
	default:
		return fmt.Sprintf("%s %s %d", name, p.Type, p.Amount)
	}
}

func connStateLine(msg ConnStateMsg) string {
	switch msg.State {
	case "connecting":
		return dimStyle.Render("conectando…")
	case "connected":
		return goodStyle.Render("conectado")
	case "disconnected":
		if msg.Err != nil {
			return errStyle.Render("desconectado: " + msg.Err.Error())
		}
		return errStyle.Render("desconectado, reintentando…")
	default:
		return msg.State
	}
}

// appendLog pushes line onto the ring buffer, dropping the oldest entry
// past logCap.
func appendLog(log []string, line string) []string {
	return appendRing(log, line, logCap)
}

// appendRing pushes line onto buf, dropping the oldest entry past cap.
// Shared by the bottom log and the three side-panel buffers.
func appendRing(buf []string, line string, cap int) []string {
	buf = append(buf, line)
	if len(buf) > cap {
		buf = buf[len(buf)-cap:]
	}
	return buf
}
