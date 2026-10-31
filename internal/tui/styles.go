package tui

import (
	"strings"

	lipgloss "charm.land/lipgloss/v2"

	"github.com/whoAngeel/pokerhub/internal/protocol"
)

// Color palette. Kept to a handful of named colors so the table reads
// clearly over SSH/tmux and over a plain xterm-256color profile (the common
// case when watching through a Cloudflare tunnel from a machine that isn't
// the developer's own terminal).
var (
	colorRed      = lipgloss.Color("9")  // hearts/diamonds
	colorWhite    = lipgloss.Color("15") // spades/clubs
	colorDim      = lipgloss.Color("8")  // folded, secondary text
	colorAccent   = lipgloss.Color("11") // to-act / highlight
	colorGood     = lipgloss.Color("10") // all_in / winner
	colorBorder   = lipgloss.Color("6")
	colorBorderOk = lipgloss.Color("11")
)

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(colorAccent)
	footerStyle = lipgloss.NewStyle().Foreground(colorDim)
	dimStyle    = lipgloss.NewStyle().Foreground(colorDim)
	boldStyle   = lipgloss.NewStyle().Bold(true)
	redStyle    = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	whiteStyle  = lipgloss.NewStyle().Foreground(colorWhite).Bold(true)
	goodStyle   = lipgloss.NewStyle().Foreground(colorGood).Bold(true)
	errStyle    = lipgloss.NewStyle().Foreground(colorRed).Bold(true)

	seatBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)
	seatBoxActiveStyle = seatBoxStyle.BorderForeground(colorBorderOk).Bold(true)
	seatBoxFoldedStyle = seatBoxStyle.BorderForeground(colorDim)

	boardBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colorAccent).
			Padding(0, 2).
			Align(lipgloss.Center)

	logBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	panelBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	seatBoxWinnerStyle = seatBoxStyle.BorderForeground(colorGood).Bold(true)
)

// truncate shortens s to at most width columns (lipgloss.Width-measured),
// appending "…" when it had to cut -- used for single-line, non-scrolling
// surfaces (header, tab bar, footer) where wrapping isn't an option
// (docs/TUI_PLAN.md T2: "never let text overflow its box (truncate with
// '…')"). Multi-line scrollable surfaces (chat/reasoning/memoria) wrap
// instead, see wrapToWidth in view_side.go.
func truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	out := []rune(s)
	for len(out) > 0 && lipgloss.Width(string(out)+"…") > width {
		out = out[:len(out)-1]
	}
	return string(out) + "…"
}

// suitStyle returns the colored style for a card's suit byte: red for
// hearts/diamonds, white for spades/clubs (PROTOCOL.md §3 Card format:
// rank + suit, suit in "shdc").
func suitStyle(suit byte) lipgloss.Style {
	switch suit {
	case 'h', 'd':
		return redStyle
	default:
		return whiteStyle
	}
}

// renderCard renders one Card ("Ah", "Td", ...) with suit coloring, or the
// ascii-safe rank+letter form when ascii is true (still colored).
func renderCard(c protocol.Card, ascii bool) string {
	s := string(c)
	if len(s) != 2 {
		return s
	}
	rank, suit := s[0], s[1]
	glyph := string(suit)
	if !ascii {
		switch suit {
		case 's':
			glyph = "♠"
		case 'h':
			glyph = "♥"
		case 'd':
			glyph = "♦"
		case 'c':
			glyph = "♣"
		}
	}
	return suitStyle(suit).Render(string(rank) + glyph)
}

// renderCards joins a seat's hole cards (or a board) with a space,
// rendering a hidden back for each nil/undealt slot. want is how many card
// backs to show when cards is empty but the seat is known to be dealt in
// (e.g. hole cards not yet revealed to this role) -- unused for the
// spectator-only T1 client (which always sees every hole card), kept for
// T3's player view to reuse without a signature change.
func renderCards(cards []protocol.Card, ascii bool) string {
	if len(cards) == 0 {
		return dimStyle.Render("—")
	}
	parts := make([]string, len(cards))
	for i, c := range cards {
		parts[i] = renderCard(c, ascii)
	}
	return strings.Join(parts, " ")
}

// hiddenCards renders n face-down card backs ("🂠 🂠" or "?? ??" in --ascii).
func hiddenCards(n int, ascii bool) string {
	back := "🂠"
	if ascii {
		back = "??"
	}
	parts := make([]string, n)
	for i := range parts {
		parts[i] = back
	}
	return dimStyle.Render(strings.Join(parts, " "))
}

// dealerMark is the dealer-button glyph (or "(D)" in --ascii).
func dealerMark(ascii bool) string {
	if ascii {
		return "(D)"
	}
	return "ⓓ"
}
