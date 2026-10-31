package table

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/whoAngeel/pokerhub/internal/engine"
)

// ANSI color codes, used only when the printer was built with color=true.
const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiMagenta = "\x1b[35m"
	ansiCyan    = "\x1b[36m"
	ansiGray    = "\x1b[90m"
)

// Printer is a terminal Sink: a readable Spanish log of the session, in
// ANSI color when the destination is a TTY (PLAN/task: spectator view for
// `pokerd sim`).
type Printer struct {
	w     io.Writer
	color bool

	names map[int]string

	// latency bookkeeping for the end-of-run summary (PLAN §8: per-agent
	// latency). Only reasoning events carry Meta with a non-fallback
	// latency sample; see turn.go's comment on why agent_error doesn't
	// double up on the same sample.
	latency map[string][]int64
	order   []string // first-seen order of names with at least one sample
}

// IsTerminal reports whether f is a character device (a real terminal), the
// stdlib-only way to decide whether to emit ANSI color codes.
func IsTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// NewPrinter builds a Printer writing to w. color should normally be
// IsTerminal(os.Stdout).
func NewPrinter(w io.Writer, color bool) *Printer {
	return &Printer{w: w, color: color, names: map[int]string{}, latency: map[string][]int64{}}
}

func (p *Printer) c(code, s string) string {
	if !p.color {
		return s
	}
	return code + s + ansiReset
}

func (p *Printer) nameOf(seat int) string {
	if n, ok := p.names[seat]; ok {
		return n
	}
	return fmt.Sprintf("Asiento%d", seat)
}

// Emit implements Sink.
func (p *Printer) Emit(e Event) {
	switch e.Type {
	case EvHandStart:
		p.onHandStart(e)
	case EvRebuy:
		fmt.Fprintf(p.w, "%s\n", p.c(ansiYellow, fmt.Sprintf("  (recompra) %s vuelve a %d fichas", p.nameOf(e.Seat), e.Amount)))
	case EvBlind:
		fmt.Fprintf(p.w, "  %s publica %d\n", p.nameOf(e.Seat), e.Amount)
	case EvHoleCards:
		fmt.Fprintf(p.w, "  %s: %s\n", p.nameOf(e.Seat), p.c(ansiCyan, cardsString(e.Cards)))
	case EvStreet:
		p.onStreet(e)
	case EvTurn:
		// Silent: "thinking" below is the visible cue; "turn" mainly
		// matters to a WS client that needs legal_actions/to_call.
	case EvThinking:
		fmt.Fprintf(p.w, "%s\n", p.c(ansiGray, fmt.Sprintf("  %s está pensando...", p.nameOf(e.Seat))))
	case EvAction:
		p.onAction(e)
	case EvChat:
		fmt.Fprintf(p.w, "  %s %s\n", p.nameOf(e.Seat), p.c(ansiMagenta, fmt.Sprintf("dice: %q", e.Say)))
	case EvReasoning:
		p.onReasoning(e)
	case EvShowdown:
		p.onShowdown(e)
	case EvPotAwarded:
		p.onPotAwarded(e)
	case EvHandEnd:
		p.onHandEnd(e)
	case EvAgentError:
		fmt.Fprintf(p.w, "%s\n", p.c(ansiRed, fmt.Sprintf("  [agent_error] %s: %s", p.nameOf(e.Seat), e.Cause)))
	}
}

func (p *Printer) onHandStart(e Event) {
	for seat, name := range e.SeatNames {
		p.names[seat] = name
	}
	header := fmt.Sprintf("═══ Mano #%d — Botón: %s | Ciegas %d/%d ═══", e.HandID, p.nameOf(e.Button), e.SmallBlind, e.BigBlind)
	fmt.Fprintf(p.w, "\n%s\n", p.c(ansiBold+ansiBlue, header))

	seats := make([]int, 0, len(e.Stacks))
	for seat := range e.Stacks {
		seats = append(seats, seat)
	}
	sort.Ints(seats)
	for _, seat := range seats {
		fmt.Fprintf(p.w, "  %s: %d fichas\n", p.nameOf(seat), e.Stacks[seat])
	}
}

func (p *Printer) onStreet(e Event) {
	label := strings.ToUpper(string(e.Street))
	fmt.Fprintf(p.w, "%s\n", p.c(ansiBold, fmt.Sprintf("— %s — mesa: %s", label, cardsString(e.Board))))
}

func (p *Printer) onAction(e Event) {
	verb := actionVerbEs(e.Action.Type)
	amount := ""
	if e.Action.Type == engine.Bet || e.Action.Type == engine.Raise || e.Action.Type == engine.Call || e.Action.Type == engine.AllIn {
		amount = fmt.Sprintf(" %d", e.Action.Amount)
	}
	color := ansiGreen
	if e.Action.Type == engine.Fold {
		color = ansiRed
	}
	fmt.Fprintf(p.w, "%s\n", p.c(color, fmt.Sprintf("  %s %s%s  (bote: %d, fichas: %d)", p.nameOf(e.Seat), verb, amount, e.PotAfter, e.StackAfter)))
}

func (p *Printer) onReasoning(e Event) {
	if e.Reasoning != nil {
		block := fmt.Sprintf(
			"      mano: %s\n      odds: %s\n      lecturas: %s\n      plan: %s",
			e.Reasoning.Hand, e.Reasoning.Odds, e.Reasoning.Reads, e.Reasoning.Plan,
		)
		fmt.Fprintf(p.w, "%s\n", p.c(ansiDim, block))
	}
	extra := fmt.Sprintf("      [%dms", e.Meta.LatencyMS)
	if len(e.Meta.Warnings) > 0 {
		extra += ", avisos: " + strings.Join(e.Meta.Warnings, ",")
	}
	extra += "]"
	fmt.Fprintf(p.w, "%s\n", p.c(ansiDim, extra))

	if e.Meta.LatencyMS > 0 {
		p.recordLatency(p.nameOf(e.Seat), e.Meta.LatencyMS)
	}
}

func (p *Printer) recordLatency(name string, ms int64) {
	if _, ok := p.latency[name]; !ok {
		p.order = append(p.order, name)
	}
	p.latency[name] = append(p.latency[name], ms)
}

func (p *Printer) onShowdown(e Event) {
	for _, r := range e.Reveals {
		fmt.Fprintf(p.w, "  %s muestra %s %s\n", p.nameOf(r.Seat), p.c(ansiCyan, cardsString(r.Cards)), p.c(ansiYellow, "→ "+r.Rank))
	}
}

func (p *Printer) onPotAwarded(e Event) {
	for _, pot := range e.Pots {
		names := make([]string, len(pot.Winners))
		for i, w := range pot.Winners {
			names[i] = p.nameOf(w)
		}
		fmt.Fprintf(p.w, "%s\n", p.c(ansiGreen, fmt.Sprintf("  bote %d → %s", pot.Amount, strings.Join(names, ", "))))
	}
}

func (p *Printer) onHandEnd(e Event) {
	seats := make([]int, 0, len(e.Stacks))
	for seat := range e.Stacks {
		seats = append(seats, seat)
	}
	sort.Ints(seats)
	fmt.Fprintln(p.w, p.c(ansiBold, "  — fin de mano —"))
	for _, seat := range seats {
		fmt.Fprintf(p.w, "  %s: %d fichas\n", p.nameOf(seat), e.Stacks[seat])
	}
}

// PrintLatencySummary writes the per-agent average/count latency summary
// (PLAN task F2: "per-agent latency summary at the end"). Call it once,
// after Session.Run returns.
func (p *Printer) PrintLatencySummary() {
	if len(p.order) == 0 {
		return
	}
	fmt.Fprintf(p.w, "\n%s\n", p.c(ansiBold, "═══ Latencia por agente ═══"))
	for _, name := range p.order {
		samples := p.latency[name]
		var sum int64
		for _, v := range samples {
			sum += v
		}
		avg := sum / int64(len(samples))
		fmt.Fprintf(p.w, "  %s: %d decisiones, media %dms\n", name, len(samples), avg)
	}
}

func cardsString(cards []engine.Card) string {
	parts := make([]string, len(cards))
	for i, c := range cards {
		parts[i] = string(c)
	}
	return strings.Join(parts, " ")
}

func actionVerbEs(t engine.ActionType) string {
	switch t {
	case engine.Fold:
		return "se retira"
	case engine.Check:
		return "pasa"
	case engine.Call:
		return "iguala"
	case engine.Bet:
		return "apuesta"
	case engine.Raise:
		return "sube a"
	case engine.AllIn:
		return "va all-in con"
	default:
		return string(t)
	}
}
