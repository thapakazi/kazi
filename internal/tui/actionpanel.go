package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// actionPanelMaxRows caps the expanded log box height (in log lines).
const actionPanelMaxRows = 10

// actionRows is the log-box height when expanded: a box-like minimum so it
// reads as a panel even with a couple of lines, up to the max, and never more
// than a third of the screen.
func (m Model) actionRows() int {
	rows := len(m.actionLines)
	if rows < 5 {
		rows = 5
	}
	if rows > actionPanelMaxRows {
		rows = actionPanelMaxRows
	}
	if cap := m.height / 3; cap >= 1 && rows > cap {
		rows = cap
	}
	return rows
}

// actionVisible returns the window of log lines shown for the current scroll
// offset: scroll 0 shows the latest `rows`, larger offsets reveal older history.
func (m Model) actionVisible(rows int) []string {
	n := len(m.actionLines)
	if n <= rows {
		return m.actionLines
	}
	scroll := m.actionScroll
	if max := n - rows; scroll > max {
		scroll = max
	}
	if scroll < 0 {
		scroll = 0
	}
	end := n - scroll
	return m.actionLines[end-rows : end]
}

// scrollAction moves the Action panel's history window by delta lines (positive
// = toward older), clamped to the available range.
func (m *Model) scrollAction(delta int) {
	n := len(m.actionLines)
	maxScroll := n - m.actionRows()
	if maxScroll < 0 {
		maxScroll = 0
	}
	m.actionScroll += delta
	if m.actionScroll < 0 {
		m.actionScroll = 0
	}
	if m.actionScroll > maxScroll {
		m.actionScroll = maxScroll
	}
}

// actionPanelHeight is the rows the panel occupies: 0 idle, 1 collapsed (just
// the title bar), or the title bar plus the log box when expanded. bodyHeight
// subtracts this so the panel never overlaps.
func (m Model) actionPanelHeight() int {
	if m.actionTitle == "" {
		return 0
	}
	if !m.actionOpen {
		return 1
	}
	return 1 + m.actionRows()
}

// renderActionPanel draws the "Action Logs" title bar and, when expanded, the
// captured compose output in a fixed-height box below it. The row count exactly
// matches actionPanelHeight so the surrounding layout stays aligned.
func (m Model) renderActionPanel(width int) string {
	if m.actionTitle == "" {
		return ""
	}
	arrow, hint := "▸", "a:expand"
	if m.actionOpen {
		arrow, hint = "▾", "a:collapse"
	}
	status := ""
	if m.actionRunning {
		status = " …"
	}
	left := fmt.Sprintf("%s Action Logs   %s%s", arrow, m.actionTitle, status)
	pad := width - lipgloss.Width(left) - lipgloss.Width(hint)
	if pad < 1 {
		pad = 1
	}
	bar := m.st.actionBar.Width(width).Render(truncate(left+strings.Repeat(" ", pad)+hint, width))
	if !m.actionOpen {
		return bar
	}

	// Log box: a scrollable window over the history; blank rows pad the box out.
	rows := m.actionRows()
	shown := m.actionVisible(rows)
	var b strings.Builder
	b.WriteString(bar)
	for i := 0; i < rows; i++ {
		b.WriteByte('\n')
		if i < len(shown) {
			b.WriteString(m.st.actionLine.Render("  " + truncate(shown[i], width-2)))
		}
	}
	return b.String()
}
