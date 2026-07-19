package tui

import (
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// segment is a clickable horizontal span [start,end) in rendered cell columns,
// tagged with an id the mouse handler dispatches on.
type segment struct {
	id         string
	start, end int
}

// segBuilder accumulates styled text while recording the cell-column span of
// each tagged piece, so the renderer and the mouse hit-tester share one source
// of truth for widths. An empty id means "not clickable" (spacers, prefixes).
type segBuilder struct {
	sb   strings.Builder
	col  int
	segs []segment
}

func (b *segBuilder) push(id, text string) {
	w := lipgloss.Width(text)
	if id != "" {
		b.segs = append(b.segs, segment{id: id, start: b.col, end: b.col + w})
	}
	b.sb.WriteString(text)
	b.col += w
}

func (b *segBuilder) String() string { return b.sb.String() }

// segAt returns the id of the span containing column x, or "".
func segAt(segs []segment, x int) string {
	for _, s := range segs {
		if x >= s.start && x < s.end {
			return s.id
		}
	}
	return ""
}

// handleMouse routes wheel and left-click events to selection, tab, and keybar
// targets. Coordinates resolve against the same geometry View draws with (the
// view.go layout constants), so a click lands on the cell the user sees. Mouse
// is inert while the filter input is active; while the help overlay is up, a
// left click dismisses it.
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || m.filtering {
		return m, nil
	}
	if m.help {
		if msg.Button == tea.MouseButtonLeft {
			m.help = false
		}
		return m, nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelDown:
		if m.actionOpen && m.inActionPanel(msg.Y) {
			m.scrollAction(-1)
			return m, nil
		}
		return m.wheel(1)
	case tea.MouseButtonWheelUp:
		if m.actionOpen && m.inActionPanel(msg.Y) {
			m.scrollAction(1)
			return m, nil
		}
		return m.wheel(-1)
	case tea.MouseButtonLeft:
		return m.click(msg.X, msg.Y)
	}
	return m, nil
}

// inActionPanel reports whether screen row y falls within the Action panel,
// which sits just below the body and above the keybar.
func (m Model) inActionPanel(y int) bool {
	ph := m.actionPanelHeight()
	if ph == 0 {
		return false
	}
	top := bodyTop + m.bodyHeight()
	return y >= top && y < top+ph
}

// wheel scrolls the active list: catalog templates in Catalog mode, else the
// sidebar selection.
func (m Model) wheel(delta int) (tea.Model, tea.Cmd) {
	if m.mode == modeCatalog {
		m.moveCatSel(delta)
		return m, nil
	}
	m.moveSel(delta)
	return m, m.navCmd()
}

// click dispatches a left click at screen (x, y) to the keybar, the sidebar, or
// the detail pane based on the layout geometry.
func (m Model) click(x, y int) (tea.Model, tea.Cmd) {
	// Keybar occupies the bottom row.
	if y == m.height-keybarH {
		_, segs := m.keybarSegs()
		switch segAt(segs, x) {
		case "mode":
			m.toggleMode()
			return m, m.navCmd()
		case "help":
			m.help = true
		case "action":
			if m.actionTitle != "" {
				m.actionOpen = !m.actionOpen
			}
		}
		// "act:*" bindings are wave-2 dispatch; ignored for now.
		return m, nil
	}
	// Body rows.
	if y < bodyTop || y >= bodyTop+m.bodyHeight() {
		return m, nil
	}
	if x < sidebarWidth {
		return m.clickSidebar(y)
	}
	return m.clickDetail(x, y)
}

// clickSidebar selects the stack row under the click (headers are inert).
func (m Model) clickSidebar(y int) (tea.Model, tea.Cmd) {
	idx := y - bodyTop
	if idx < 0 || idx >= len(m.rows) || m.rows[idx].kind == rowHeader {
		return m, nil
	}
	m.focus = focusSidebar
	m.sel = idx
	return m, m.navCmd()
}

// clickDetail resolves a click in the detail pane: a catalog row in Catalog
// mode, or a tab in the header line when a stack is selected.
func (m Model) clickDetail(x, y int) (tea.Model, tea.Cmd) {
	if m.mode == modeCatalog {
		// renderCatalog draws a title line + blank, then one row per template.
		idx := y - bodyTop - 2
		if idx >= 0 && idx < len(m.templates) {
			m.catSel = idx
		}
		return m, nil
	}
	r := m.selectedRow()
	if r == nil || r.kind != rowStack || r.stack == nil {
		return m, nil
	}
	// The tab header is the detail pane's first line.
	if y == bodyTop {
		_, segs := m.tabSegs()
		if id := segAt(segs, x-detailContentX0); strings.HasPrefix(id, "tab:") {
			if i, err := strconv.Atoi(id[len("tab:"):]); err == nil {
				m.focus = focusDetail
				m.tab = detailTab(i)
				return m, m.navCmd()
			}
		}
	}
	return m, nil
}

// moveCatSel moves the catalog selection by delta, clamped to the list.
func (m *Model) moveCatSel(delta int) {
	if len(m.templates) == 0 {
		return
	}
	m.catSel += delta
	if m.catSel < 0 {
		m.catSel = 0
	}
	if m.catSel >= len(m.templates) {
		m.catSel = len(m.templates) - 1
	}
}
