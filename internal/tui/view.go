package tui

import (
	"github.com/charmbracelet/lipgloss"
)

// Layout geometry. These constants are shared by the renderer (view) and the
// mouse hit-tester (mouse.go) so click coordinates map back to the same cells
// the frame drew. Changing any of them changes both sides at once.
const (
	sidebarWidth = 22 // fixed left-pane width
	statusBarH   = 1  // top status bar occupies row 0
	keybarH      = 1  // contextual keybar occupies the bottom row
	detailBorder = 1  // detail pane's left divider column (│)
	detailPadL   = 2  // detail pane's left content padding
)

// bodyTop is the first screen row of the sidebar/detail body (below the bar).
const bodyTop = statusBarH

// detailContentX0 is the screen column where the detail pane's text begins:
// past the sidebar, its divider border, and its left padding.
const detailContentX0 = sidebarWidth + detailBorder + detailPadL

// bodyHeight is the number of rows available to the body: everything between
// the status bar and the keybar, minus the Action panel when present. Clamped
// to at least 1.
func (m Model) bodyHeight() int {
	h := m.height - statusBarH - keybarH - m.actionPanelHeight()
	if h < 1 {
		h = 1
	}
	return h
}

// detailWidth is the width of the detail pane (the rest after the sidebar).
func (m Model) detailWidth() int {
	w := m.width - sidebarWidth
	if w < 20 {
		w = 20
	}
	return w
}

// View composes the full-screen frame: a full-width status bar, the body
// (sidebar | detail, or the help overlay) filling all remaining height, and a
// full-width contextual keybar pinned to the bottom row. While filtering, the
// keybar row shows the filter input instead — so the frame height never shifts.
func (m Model) View() string {
	bodyH := m.bodyHeight()

	bar := m.st.statusBar.Width(m.width).Render(m.statusBarLine())

	var bottom string
	if m.filtering {
		bottom = m.st.keybar.Width(m.width).Render("/" + m.filter + "▏")
	} else {
		bottom = m.st.keybar.Width(m.width).Render(m.keybarLine())
	}

	var body string
	if m.help {
		body = lipgloss.Place(m.width, bodyH, lipgloss.Center, lipgloss.Center, m.renderHelp())
	} else {
		dw := m.detailWidth()
		sidebar := m.st.sidebar.Width(sidebarWidth).Height(bodyH).Render(m.sidebarLines())
		// lipgloss Width() sets the content+padding box; the left border is added
		// outside it. Subtract the border column so sidebar+detail == full width.
		detail := m.st.detail.Width(dw - detailBorder).Height(bodyH).Render(m.detailBody(dw - detailBorder - detailPadL*2))
		body = lipgloss.JoinHorizontal(lipgloss.Top, sidebar, detail)
	}

	segments := []string{bar, body}
	if panel := m.renderActionPanel(m.width); panel != "" {
		segments = append(segments, panel)
	}
	segments = append(segments, bottom)
	frame := lipgloss.JoinVertical(lipgloss.Left, segments...)

	// The fullscreen Logs popup floats over the dashboard (margins on every
	// side), giving long log lines the full terminal width while the status bar
	// and sidebar edges stay visible behind it.
	if m.onLogsTab() && m.logFullscreen && !m.help {
		frame = overlay(frame, m.renderLogsFullscreen(), m.width, m.height)
	}
	// The Stats tab shares the same fullscreen contract for bigger graphs.
	if m.onStatsTab() && m.statsFullscreen && !m.help {
		frame = overlay(frame, m.renderStatsFullscreen(), m.width, m.height)
	}

	// A modal or input form floats over the dashboard rather than replacing it,
	// so context (which stack, its state) stays visible behind the popup.
	if m.modal.active {
		frame = overlay(frame, m.renderModal(), m.width, m.height)
	} else if m.form.active {
		frame = overlay(frame, m.renderForm(), m.width, m.height)
	}
	return frame
}
