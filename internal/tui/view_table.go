package tui

import (
	"fmt"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lipgloss "charm.land/lipgloss/v2"

	"github.com/whoAngeel/pokerhub/internal/protocol"
)

// compactWidth/compactHeight are docs/TUI_PLAN.md's threshold for the
// compact (vertical list) layout instead of the seats-around-the-board one.
const (
	compactWidth  = 110
	compactHeight = 32
)

// View renders the whole screen: header, table (or compact seat list), log
// and footer. It is the one place that calls TableState.Snapshot() to get
// a renderable god-view of the table -- reusing the exact same struct the
// wire protocol's "snapshot" event uses, rather than re-deriving board/pot/
// seats bookkeeping here.
func (m Model) View() tea.View {
	if m.quitting {
		return tea.NewView("")
	}
	v := m.renderView()
	v.AltScreen = true
	return v
}

// renderView builds the View's content; split out from View so AltScreen
// only has to be set in one place above.
func (m Model) renderView() tea.View {

	var snap protocol.SnapshotPayload
	haveSnap := false
	if m.ts != nil {
		if env, err := m.ts.Snapshot(); err == nil {
			if env.Decode(&snap) == nil {
				haveSnap = true
			}
		}
	}

	header := m.renderHeader(snap, haveSnap)
	footer := m.renderFooter()

	if !haveSnap {
		body := dimStyle.Render("esperando snapshot inicial del servidor…")
		content := lipgloss.JoinVertical(lipgloss.Left, header, "", body, "", m.renderLog(6), footer)
		return tea.NewView(content)
	}

	panelBoxW, panelContentW, panelH, showPanel := m.sidePanelGeometry()
	wide := m.width >= wideThreshold

	tableModel := m
	if showPanel && wide {
		tableModel.width = m.width - panelBoxW - 2
	}
	var table string
	if tableModel.width < compactWidth || tableModel.height < compactHeight {
		table = tableModel.renderCompact(snap)
	} else {
		table = tableModel.renderTable(snap)
	}

	body := table
	if showPanel {
		panel := m.renderSidePanel(panelBoxW, panelH)
		_ = panelContentW
		if wide {
			body = lipgloss.JoinHorizontal(lipgloss.Top, table, "  ", panel)
		} else {
			body = lipgloss.JoinVertical(lipgloss.Left, table, panel)
		}
	}

	if os.Getenv("TUI_DEBUG_LAYOUT") != "" {
		fmt.Fprintf(os.Stderr, "DEBUG layout: m.height=%d panelH(want)=%d table.H=%d panel.H=%d body.H=%d header.H=%d footer.H=%d\n",
			m.height, panelH, lipgloss.Height(table), lipgloss.Height(m.renderSidePanel(panelBoxW, panelH)), lipgloss.Height(body), lipgloss.Height(header), lipgloss.Height(footer))
	}
	logLines := m.height - lipgloss.Height(body) - lipgloss.Height(header) - lipgloss.Height(footer) - 1
	if logLines < 3 {
		logLines = 3
	}
	content := lipgloss.JoinVertical(lipgloss.Left, header, body, m.renderLog(logLines), footer)
	return tea.NewView(content)
}

// renderFooter draws the bottom-most line(s): a ~3s "error" flash after a
// failed control message (docs/TUI_PLAN.md T2: "Handle the server's error
// reply by flashing it in the footer") replacing the bubbles/help short or
// full keymap view ("?" toggles which) while it's live.
func (m Model) renderFooter() string {
	if time.Now().Before(m.footerErrUntil) && m.footerErrMsg != "" {
		return errStyle.Render(truncate("⚠ "+m.footerErrMsg, m.width))
	}
	return m.help.View(m.keys)
}

// pacingBadge renders the header's pause/speed indicator (docs/TUI_PLAN.md
// T2: 'Show state in the header ("⏸ pausado", "x2")').
func (m Model) pacingBadge() string {
	if m.paused {
		return errStyle.Render(fmt.Sprintf("⏸ pausado · x%g", m.speed))
	}
	return goodStyle.Render(fmt.Sprintf("▶ x%g", m.speed))
}

// renderLog draws the bottom log box with the last n lines (or fewer).
func (m Model) renderLog(n int) string {
	lines := m.log
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	body := strings.Join(lines, "\n")
	if body == "" {
		body = dimStyle.Render("(sin eventos todavía)")
	}
	return logBoxStyle.Width(maxInt(m.width-4, 20)).Render(body)
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func (m Model) renderHeader(snap protocol.SnapshotPayload, haveSnap bool) string {
	status := connStateLine(ConnStateMsg{State: m.connState, Err: m.connErr})
	if !haveSnap {
		return headerStyle.Render("pokerd tui") + "  " + status
	}
	handNo := 0
	if snap.HandID != nil {
		handNo = *snap.HandID
	}
	title := fmt.Sprintf("Mano #%d · %s · Ciegas %d/%d · bote %d", handNo, orDash(snap.Street), snap.SmallBlind, snap.BigBlind, snap.Pot)
	return truncate(headerStyle.Render(title)+"  "+status+"  "+m.pacingBadge(), m.width)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// renderTable draws the 5-seat-around-the-board layout from
// docs/TUI_PLAN.md's mockup: two seats on top, one on each side of the
// board in the middle row, one centered below.
func (m Model) renderTable(snap protocol.SnapshotPayload) string {
	seatBySeat := make(map[int]protocol.SnapshotSeat, len(snap.Seats))
	for _, s := range snap.Seats {
		seatBySeat[s.Seat] = s
	}
	order := make([]int, len(snap.Seats))
	for i, s := range snap.Seats {
		order[i] = s.Seat
	}

	box := func(idx int) string {
		if idx >= len(order) {
			return ""
		}
		return m.renderSeatBox(seatBySeat[order[idx]], snap)
	}

	board := m.renderBoardBox(snap)

	top := lipgloss.JoinHorizontal(lipgloss.Top, box(2), strings.Repeat(" ", 4), box(3))
	mid := lipgloss.JoinHorizontal(lipgloss.Center, box(1), strings.Repeat(" ", 2), board, strings.Repeat(" ", 2), box(4))
	bottom := box(0)

	width := m.width
	if width < lipgloss.Width(mid) {
		width = lipgloss.Width(mid)
	}
	rows := []string{
		lipgloss.PlaceHorizontal(width, lipgloss.Center, top),
		lipgloss.PlaceHorizontal(width, lipgloss.Center, mid),
		lipgloss.PlaceHorizontal(width, lipgloss.Center, bottom),
	}
	return lipgloss.JoinVertical(lipgloss.Center, rows...)
}

// renderCompact draws a plain vertical seat list for small terminals
// (< 110x32) plus a one-line board/pot summary, per docs/TUI_PLAN.md's
// "modo compacto" requirement.
func (m Model) renderCompact(snap protocol.SnapshotPayload) string {
	var b strings.Builder
	boardLine := fmt.Sprintf("Board: %s   Bote: %d", renderCards(snap.Board, m.ascii), snap.Pot)
	b.WriteString(boldStyle.Render(boardLine))
	b.WriteString("\n")
	for _, s := range snap.Seats {
		marker := " "
		if s.Seat == snap.Button {
			marker = dealerMark(m.ascii)
		}
		act := " "
		if s.Seat == snap.ToAct {
			act = "▶"
		}
		line := fmt.Sprintf("%s%s %-14s %5d  bet %-4d %-9s %s", act, marker, s.Name, s.Stack, s.BetThisStreet, s.Status, renderCards(s.Hole, m.ascii))
		b.WriteString(m.styleBySeatStatus(s, snap).Render(line))
		if m.thinking != nil && *m.thinking == s.Seat {
			b.WriteString("  " + m.spin.View() + " pensando…")
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (m Model) renderBoardBox(snap protocol.SnapshotPayload) string {
	board := renderCards(snap.Board, m.ascii)
	if len(snap.Board) == 0 {
		board = dimStyle.Render("(sin board)")
	}
	content := board + "\n" + boldStyle.Render(fmt.Sprintf("bote %d", snap.Pot))
	return boardBoxStyle.Render(content)
}

// renderSeatBox draws one seat's box: name+stack, bet/status line, hole
// cards, dealer marker and a highlighted border when it's this seat's turn
// (with a spinner + "pensando…" while Model.thinking names it).
func (m Model) renderSeatBox(s protocol.SnapshotSeat, snap protocol.SnapshotPayload) string {
	style := m.styleBySeatStatus(s, snap)

	name := s.Name
	if s.Seat == snap.Button {
		name += " " + dealerMark(m.ascii)
	}
	nameLine := boldStyle.Render(fmt.Sprintf("%s %d", name, s.Stack))

	statusLine := statusLineFor(s)
	if m.thinking != nil && *m.thinking == s.Seat {
		statusLine = m.spin.View() + " pensando…"
	}
	if amt, ok := m.winnerSeats[s.Seat]; ok && time.Now().Before(m.winnerUntil) {
		statusLine = goodStyle.Render(fmt.Sprintf("gana %d", amt))
	}

	cardsLine := renderCards(s.Hole, m.ascii)

	content := lipgloss.JoinVertical(lipgloss.Center, nameLine, statusLine, cardsLine)
	return style.Render(content)
}

// statusLineFor renders the bet/status row: "bet N" while active with
// chips in, "folded"/"all_in" otherwise.
func statusLineFor(s protocol.SnapshotSeat) string {
	switch s.Status {
	case "folded":
		return dimStyle.Render("folded")
	case "all_in":
		return goodStyle.Render("all-in")
	case "sitting_out":
		return dimStyle.Render("fuera")
	default:
		if s.BetThisStreet > 0 {
			return fmt.Sprintf("bet %d", s.BetThisStreet)
		}
		return " "
	}
}

// styleBySeatStatus picks the box border style: green for ~3s after this
// seat won a pot (docs/TUI_PLAN.md T2 "Highlight winner"), highlighted
// when it's this seat's turn, dimmed when folded/sitting out, plain
// otherwise. Winner takes priority over to-act -- the two can't actually
// overlap (nobody is "to act" once the pot's been awarded) but a tie-break
// still has to pick one.
func (m Model) styleBySeatStatus(s protocol.SnapshotSeat, snap protocol.SnapshotPayload) lipgloss.Style {
	if _, ok := m.winnerSeats[s.Seat]; ok && time.Now().Before(m.winnerUntil) {
		return seatBoxWinnerStyle
	}
	switch {
	case s.Seat == snap.ToAct:
		return seatBoxActiveStyle
	case s.Status == "folded" || s.Status == "sitting_out":
		return seatBoxFoldedStyle
	default:
		return seatBoxStyle
	}
}
