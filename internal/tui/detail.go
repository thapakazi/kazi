package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/thapakazi/kazi/internal/engine"
)

// detailBody builds the right pane's content (the outer bordered style is
// applied by View). With ALL selected it is a cross-kind overview; with a stack
// selected it is the tabbed detail (Services/Logs/URLs/Config). Catalog mode
// routes elsewhere (renderCatalog).
func (m Model) detailBody(width int) string {
	if m.mode == modeCatalog {
		return m.renderCatalog(width)
	}
	r := m.selectedRow()
	if r == nil || r.kind == rowAll {
		return m.renderOverview()
	}
	if r.stack == nil {
		return "no selection"
	}
	return m.renderTabs(*r.stack, width)
}

// tabSegs builds the tab header string and the clickable column spans for each
// tab (mouse.go hit-tests these, offset by detailContentX0). Kept as one pass
// so the rendered widths and the click zones can never drift apart. No stack
// breadcrumb: the sidebar already shows which stack is selected.
func (m Model) tabSegs() (string, []segment) {
	var b segBuilder
	for i, name := range tabNames {
		var seg string
		if detailTab(i) == m.tab {
			seg = m.st.tabActive.Render(name)
		} else {
			seg = m.st.tabInactive.Render(name)
		}
		b.push("tab:"+strconv.Itoa(i), seg)
		if i < len(tabNames)-1 {
			b.push("", " │ ")
		}
	}
	return b.String(), b.segs
}

// renderOverview is the ALL cross-kind global dashboard: a flat list of every
// stack with its health glyph and running/total tally.
func (m Model) renderOverview() string {
	var b strings.Builder
	b.WriteString(m.st.tabActive.Render("Overview") + "\n\n")
	for _, r := range m.rows {
		if r.kind != rowStack {
			continue
		}
		icon := m.st.glyphStyle(r.running, r.total).Render(kindGlyph(r.selKind))
		fmt.Fprintf(&b, "%s  %-26s  %d/%d\n", icon, r.label, r.running, r.total)
	}
	if b.Len() == 0 {
		b.WriteString("no stacks")
	}
	return b.String()
}

// renderTabs draws the tab header (tabs left, selected-stack label right) plus
// the active tab body for one stack. The label makes the "focused" stack
// explicit — which stack every contextual key acts on.
func (m Model) renderTabs(s engine.StackInfo, width int) string {
	head, _ := m.tabSegs()
	label := m.st.stackLabel.Render(s.Name)
	gap := width - lipgloss.Width(head) - lipgloss.Width(s.Name)
	if gap < 1 {
		gap = 1
	}
	head += strings.Repeat(" ", gap) + label

	var body string
	switch m.tab {
	case tabServices:
		body = m.renderServices(s)
	case tabLogs:
		body = m.renderLogs(width)
	case tabEnv:
		body = m.renderEnv(width)
	case tabURLs:
		body = m.renderURLs(s)
	case tabConfig:
		body = m.renderConfig(s)
	}
	return head + "\n\n" + body
}

// renderLogs draws the M5-Log viewer for the selected stack: a derived-state
// control strip plus the scrollable, search-highlighted viewport. The heavy
// lifting lives in renderLogsPane (logs.go); this is the tab dispatch seam.
func (m Model) renderLogs(width int) string {
	return m.renderLogsPane(width)
}

// truncate clips s to at most w display cells, marking a cut with an ellipsis.
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= w {
		return s
	}
	if w == 1 {
		return "…"
	}
	return string(r[:w-1]) + "…"
}

// renderServices lists the stack's containers with state and health.
func (m Model) renderServices(s engine.StackInfo) string {
	containers := s.Containers
	if m.statusName == s.Name && len(m.statusInfo.Containers) > 0 {
		containers = m.statusInfo.Containers
	}
	if len(containers) == 0 {
		return "no services running"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%-16s %-10s %-10s %s\n", "SERVICE", "STATE", "HEALTH", "PORTS")
	for _, c := range containers {
		svc := c.Service
		if svc == "" {
			svc = c.Name
		}
		fmt.Fprintf(&b, "%-16s %-10s %-10s %s\n", svc, c.State, c.Health, c.Ports)
	}
	return b.String()
}

// renderURLs lists the stack's reachable endpoints (cached from urlsCmd).
func (m Model) renderURLs(s engine.StackInfo) string {
	if m.endpointsFor != s.Name {
		return "resolving endpoints…"
	}
	if len(m.endpoints) == 0 {
		return "no endpoints"
	}
	var b strings.Builder
	for _, e := range m.endpoints {
		loc := e.URL
		if loc == "" {
			loc = e.Target
		}
		fmt.Fprintf(&b, "%-10s %-6s → %s", e.Service, e.Kind, loc)
		if e.Note != "" {
			fmt.Fprintf(&b, "  (%s)", e.Note)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// renderConfig shows the described stack's manifest-side declarations. Per-field
// origin annotations are deferred to wave 2 (§scope).
func (m Model) renderConfig(s engine.StackInfo) string {
	if m.detailFor != s.Name {
		return "loading config…"
	}
	d := m.detail
	var b strings.Builder
	fmt.Fprintf(&b, "kind:    %s\n", d.Kind)
	if d.Source != "" {
		fmt.Fprintf(&b, "source:  %s\n", d.Source)
	}
	if d.System {
		b.WriteString("system:  true (protected)\n")
	}
	if d.Proxy != nil {
		fmt.Fprintf(&b, "proxy:   service=%s port=%d\n", d.Proxy.Service, d.Proxy.HTTPPort)
	}
	for _, e := range d.Expose {
		fmt.Fprintf(&b, "expose:  %s → %s\n", e.Service, e.Port)
	}
	if b.Len() == 0 {
		b.WriteString("no manifest (discovered stack)")
	}
	return b.String()
}

// renderCatalog draws the Catalog mode detail: the selected template's name and
// description. Try/eject actions are wave 2.
func (m Model) renderCatalog(width int) string {
	if len(m.templates) == 0 {
		return m.st.detail.Render("no templates in catalog")
	}
	var b strings.Builder
	b.WriteString(m.st.tabActive.Render("Catalog") + "\n\n")
	for i, t := range m.templates {
		marker := "  "
		if i == m.catSel {
			marker = "▸ "
		}
		src := "user"
		if t.Embedded {
			src = "embedded"
		}
		fmt.Fprintf(&b, "%s%-16s %-9s %s\n", marker, t.Name, src, t.Description)
	}
	return m.st.detail.Render(b.String())
}
