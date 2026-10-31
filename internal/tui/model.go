package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/spinner"

	"github.com/whoAngeel/pokerhub/internal/protocol"
)

// logCap is the size of the ring buffer of recent action/chat/system lines
// (docs/TUI_PLAN.md T1: "log de últimas ~200 líneas").
const logCap = 200

// Model is the Bubble Tea root model for `pokerd tui`. It keeps
// protocol.TableState (the god-view reducer this package REUSES rather
// than re-deriving -- see internal/protocol/state.go) plus the small bit of
// UI-only state TableState doesn't track: a ring of recent log lines, who
// (if anyone) is currently "thinking", connection status, terminal size and
// a couple of display flags.
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
}

// NewModel builds a Model ready to Run, before any snapshot has arrived
// (ts is nil until the first "snapshot" Envelope seeds it -- see Update).
func NewModel(ascii bool) Model {
	sp := spinner.New(spinner.WithSpinner(spinner.MiniDot))
	if ascii {
		sp = spinner.New(spinner.WithSpinner(spinner.Line))
	}
	return Model{
		connState: "connecting",
		ascii:     ascii,
		width:     110,
		height:    32,
		spin:      sp,
	}
}

// Init starts the spinner ticking (harmless even while nobody is thinking;
// View only renders it next to the seat Model.thinking names).
func (m Model) Init() tea.Cmd {
	return m.spin.Tick
}

// Update folds one Bubble Tea message into m. EnvMsg is the only message
// that touches game state, and it does so entirely through
// protocol.TableState.Apply -- Update never inspects a payload's fields
// itself beyond what's needed for the log line and the thinking cue.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit
		}
		return m, nil

	case ConnStateMsg:
		m.connState = msg.State
		m.connErr = msg.Err
		m.log = appendLog(m.log, connStateLine(msg))
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

// applyEnvelope is the one place that both feeds env into TableState and
// derives the UI-only bits (log line, thinking cue) TableState doesn't
// carry.
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
			m.log = appendLog(m.log, actionLine(p))
		}
	case protocol.TypeChat:
		var p protocol.ChatPayload
		if env.Decode(&p) == nil {
			m.log = appendLog(m.log, fmt.Sprintf("%s %s: %q", "💬", m.seatName(p.Seat), p.Say))
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
			for _, pot := range p.Pots {
				names := make([]string, len(pot.Winners))
				for i, w := range pot.Winners {
					names[i] = m.seatName(w)
				}
				m.log = appendLog(m.log, goodStyle.Render(fmt.Sprintf("bote %d → %v", pot.Amount, names)))
			}
		}
	case protocol.TypeAgentError:
		var p protocol.AgentErrorPayload
		if env.Decode(&p) == nil {
			m.log = appendLog(m.log, errStyle.Render(fmt.Sprintf("agent_error seat %d: %s (fallback=%v)", p.Seat, p.Error, p.Fallback)))
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

func actionLine(p protocol.ActionPayload) string {
	switch p.Type {
	case "fold":
		return fmt.Sprintf("seat %d se retira", p.Seat)
	case "check":
		return fmt.Sprintf("seat %d pasa", p.Seat)
	case "call":
		return fmt.Sprintf("seat %d iguala %d", p.Seat, p.Amount)
	case "bet":
		return fmt.Sprintf("seat %d apuesta %d", p.Seat, p.Amount)
	case "raise":
		return fmt.Sprintf("seat %d sube %d", p.Seat, p.Amount)
	case "all_in":
		return goodStyle.Render(fmt.Sprintf("seat %d all-in %d", p.Seat, p.Amount))
	default:
		return fmt.Sprintf("seat %d %s %d", p.Seat, p.Type, p.Amount)
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
	log = append(log, line)
	if len(log) > logCap {
		log = log[len(log)-logCap:]
	}
	return log
}
