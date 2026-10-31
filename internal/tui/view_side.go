package tui

import (
	"fmt"
	"strings"

	lipgloss "charm.land/lipgloss/v2"
)

// wideThreshold is docs/TUI_PLAN.md T2's "table left, panel right when
// width ≥ 150" cutoff. Below it the panel moves under the table and is
// hidden by default (toggle with "p").
const wideThreshold = 150

// panelBoxWide is the side panel's fixed box width in the wide layout.
const panelBoxWide = 42

// sidePanelGeometry decides whether the side panel shows at all, and the
// box width/content height it gets, from the current terminal size and the
// user's "p" toggle. contentW is the box width minus border+padding, i.e.
// what wrapLines should wrap to.
func (m Model) sidePanelGeometry() (boxW, contentW, height int, show bool) {
	wide := m.width >= wideThreshold
	show = wide || m.showPanel
	if !show {
		return 0, 0, 0, false
	}
	if wide {
		boxW = panelBoxWide
		if max := m.width / 3; boxW > max {
			boxW = max
		}
		height = m.height - 6 // header + log + footer budget, mirrors renderView
	} else {
		boxW = m.width
		height = 10
	}
	if boxW < 20 {
		boxW = 20
	}
	if height < 5 {
		height = 5
	}
	contentW = boxW - 4 // border(2) + padding(2), see panelBoxStyle
	if contentW < 10 {
		contentW = 10
	}
	return
}

// layoutPanels resizes the three tab viewports to the current geometry and
// re-wraps their buffered content to the new width. Called on every
// tea.WindowSizeMsg and on the "p" toggle; the per-event refresh*
// functions handle the common case (new line arrives, width unchanged).
func (m *Model) layoutPanels() {
	_, contentW, boxH, _ := m.sidePanelGeometry()
	// boxH is the box's total height including its 2 border rows; inside
	// that, the tab bar and its separator rule take 1 row each (see
	// renderSidePanel), so the viewport gets whatever's left.
	innerH := boxH - 2 - 2
	if innerH < 2 {
		innerH = 2
	}
	m.panelW = contentW
	m.chatVP.SetWidth(contentW)
	m.chatVP.SetHeight(innerH)
	m.reasonVP.SetWidth(contentW)
	m.reasonVP.SetHeight(innerH)
	m.memoryVP.SetWidth(contentW)
	m.memoryVP.SetHeight(innerH)
	m.refreshChat()
	m.refreshReasoning()
	m.refreshMemory()
}

// refreshChat/refreshReasoning/refreshMemory re-wrap their buffer to
// m.panelW and push it into the matching viewport, keeping the scroll
// position pinned to the bottom if it already was there (so a live chat
// keeps following unless the spectator has scrolled up to read history --
// viewport.SetContentLines itself never resets the offset, see
// charm.land/bubbles/v2/viewport).
func (m *Model) refreshChat() {
	atBottom := m.chatVP.AtBottom()
	m.chatVP.SetContentLines(renderChatTab(m.chatLines, m.panelW))
	if atBottom {
		m.chatVP.GotoBottom()
	}
}

func (m *Model) refreshReasoning() {
	atBottom := m.reasonVP.AtBottom()
	m.reasonVP.SetContentLines(renderReasoningTab(m.reasonBuf, m.agentErrs, m.panelW))
	if atBottom {
		m.reasonVP.GotoBottom()
	}
}

func (m *Model) refreshMemory() {
	atBottom := m.memoryVP.AtBottom()
	m.memoryVP.SetContentLines(renderMemoryTab(m.memoLines, m.panelW))
	if atBottom {
		m.memoryVP.GotoBottom()
	}
}

// wrapToWidth word-wraps s to width columns (lipgloss.Width-measured, so
// multi-cell emoji/wide glyphs in chat/reasoning text never overflow the
// panel -- docs/TUI_PLAN.md T2: "Use lipgloss.Width for emoji-safe
// wrapping").
func wrapToWidth(s string, width int) []string {
	if width < 1 {
		width = 1
	}
	rendered := lipgloss.NewStyle().Width(width).Render(s)
	return strings.Split(rendered, "\n")
}

// wrapLines wraps each line of lines independently (no reflow across
// lines) and flattens the result.
func wrapLines(lines []string, width int) []string {
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, wrapToWidth(l, width)...)
	}
	return out
}

// renderChatTab/renderMemoryTab render their ring buffer as wrapped lines,
// or a placeholder when empty.
func renderChatTab(lines []string, width int) []string {
	if len(lines) == 0 {
		return []string{dimStyle.Render("(sin chat todavía)")}
	}
	return wrapLines(lines, width)
}

func renderMemoryTab(lines []string, width int) []string {
	if len(lines) == 0 {
		return []string{dimStyle.Render("(sin notas de memoria todavía)")}
	}
	return wrapLines(lines, width)
}

// formatLatency renders a reasoning event's latency as "1.1s" (meta.latency_ms).
func formatLatency(ms int64) string {
	return fmt.Sprintf("%.1fs", float64(ms)/1000)
}

// renderReasoningBlock renders one reasoningEntry as hand/odds/reads/plan
// lines wrapped to width, latency + a dimmed "[fallback]"/warnings marker
// (docs/TUI_PLAN.md T2: "last N reasoning events: seat name, latency,
// hand/odds/reads/plan wrapped to panel width, warnings dimmed; mark
// fallback").
func renderReasoningBlock(e reasoningEntry, width int) []string {
	header := fmt.Sprintf("🧠 %s (%s)", e.seatName, formatLatency(e.latencyMS))
	if e.fallback {
		header += " " + errStyle.Render("[fallback]")
	}
	lines := []string{boldStyle.Render(truncate(header, width))}
	lines = append(lines, wrapToWidth("hand: "+orDash(e.hand), width)...)
	lines = append(lines, wrapToWidth("odds: "+orDash(e.odds), width)...)
	lines = append(lines, wrapToWidth("reads: "+orDash(e.reads), width)...)
	lines = append(lines, wrapToWidth("plan: "+orDash(e.plan), width)...)
	if len(e.warnings) > 0 {
		for _, l := range wrapToWidth("avisos: "+strings.Join(e.warnings, ", "), width) {
			lines = append(lines, dimStyle.Render(l))
		}
	}
	return lines
}

func renderReasoningTab(entries []reasoningEntry, agentErrs []string, width int) []string {
	var out []string
	for i, e := range entries {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, renderReasoningBlock(e, width)...)
	}
	if len(agentErrs) > 0 {
		if len(out) > 0 {
			out = append(out, "")
		}
		out = append(out, boldStyle.Render(truncate("Diagnóstico de agentes:", width)))
		for _, l := range agentErrs {
			for _, wl := range wrapToWidth(l, width) {
				out = append(out, dimStyle.Render(wl))
			}
		}
	}
	if len(out) == 0 {
		out = []string{dimStyle.Render("(sin razonamientos todavía -- solo visible para spectator)")}
	}
	return out
}

// renderTabBar highlights the active tab's name in the "Chat ·
// Razonamiento · Memoria" header row.
func (m Model) renderTabBar(width int) string {
	names := []sideTab{tabChat, tabReasoning, tabMemory}
	parts := make([]string, len(names))
	for i, t := range names {
		if t == m.activeTab {
			parts[i] = boldStyle.Foreground(colorAccent).Render(t.String())
		} else {
			parts[i] = dimStyle.Render(t.String())
		}
	}
	return truncate(strings.Join(parts, "  ·  "), width)
}

// renderSidePanel draws the bordered panel box: tab bar, a separator rule,
// then the active tab's viewport.
func (m Model) renderSidePanel(boxW, boxH int) string {
	contentW := boxW - 4
	if contentW < 1 {
		contentW = 1
	}
	tabBar := m.renderTabBar(contentW)
	rule := dimStyle.Render(strings.Repeat("─", contentW))

	var body string
	switch m.activeTab {
	case tabChat:
		body = m.chatVP.View()
	case tabReasoning:
		body = m.reasonVP.View()
	case tabMemory:
		body = m.memoryVP.View()
	}

	content := lipgloss.JoinVertical(lipgloss.Left, tabBar, rule, body)
	return panelBoxStyle.Width(contentW).Height(boxH - 2).Render(content)
}
