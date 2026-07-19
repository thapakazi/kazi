package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/thapakazi/kazi/internal/engine"
)

// isSystem reports whether a stack is the protected system stack (kazi-proxy).
// The engine has no "system" kind; the system stack surfaces by name.
func isSystem(s engine.StackInfo) bool {
	return s.Name == "kazi-proxy" || s.Project == "kazi-proxy"
}

// buildRows flattens stacks + loose containers into a single selectable list,
// sorted by kind (registered → discovered → unmanaged → system) with no group
// headers: each row carries a kind icon instead (see kindGlyph). A kazi-proxy
// stack is pulled out of its kind and sorted under SYSTEM. Rows are filtered by
// the active filter string.
func buildRows(stacks []engine.StackInfo, loose []engine.ContainerInfo, filter string) []sidebarRow {
	var registered, discovered, system []engine.StackInfo
	for _, s := range stacks {
		switch {
		case isSystem(s):
			system = append(system, s)
		case s.Kind == engine.KindRegistered:
			registered = append(registered, s)
		case s.Kind == engine.KindDiscovered:
			discovered = append(discovered, s)
		}
	}

	rows := []sidebarRow{{kind: rowAll, label: "ALL"}}

	match := func(name string) bool {
		if filter == "" {
			return true
		}
		return strings.Contains(strings.ToLower(name), strings.ToLower(filter))
	}

	addGroup := func(group []engine.StackInfo, sk selKind) {
		for i := range group {
			s := group[i]
			if !match(s.Name) {
				continue
			}
			rows = append(rows, sidebarRow{
				kind:    rowStack,
				label:   s.Name,
				stack:   &s,
				selKind: sk,
				running: s.Running,
				total:   s.Total,
			})
		}
	}

	addGroup(registered, selStack)
	addGroup(discovered, selDiscovered)

	// Unmanaged loose containers: each is its own single-container row.
	for i := range loose {
		c := loose[i]
		if !match(c.Name) {
			continue
		}
		running := 0
		if c.State == "running" {
			running = 1
		}
		si := engine.StackInfo{Name: c.Name, Kind: engine.KindUnmanaged, Running: running, Total: 1,
			Containers: []engine.ContainerInfo{c}}
		rows = append(rows, sidebarRow{
			kind: rowStack, label: c.Name, stack: &si,
			selKind: selUnmanaged, running: running, total: 1,
		})
	}

	addGroup(system, selSystem)

	return rows
}

// kindGlyph is the per-row icon whose shape encodes the stack's kind; its color
// (applied by the caller via glyphStyle) encodes health. This replaces the old
// group headers — one glyph per row, list sorted by kind.
func kindGlyph(sk selKind) string {
	switch sk {
	case selStack:
		return "◆" // registered (has a manifest)
	case selDiscovered:
		return "◇" // discovered compose project
	case selUnmanaged:
		return "□" // loose/unmanaged container
	case selSystem:
		return "⬢" // kazi system stack
	default:
		return "·"
	}
}

// firstSelectable returns the index of the first selectable row (ALL, index 0).
func firstSelectable(rows []sidebarRow) int {
	for i, r := range rows {
		if r.kind != rowHeader {
			return i
		}
	}
	return 0
}

// sidebarLines builds the grouped sidebar content (health glyphs, selection);
// the outer fixed-width/height style is applied by View. Every row occupies
// exactly one line, so a click at body row i maps straight to m.rows[i]
// (mouse.go relies on this).
func (m Model) sidebarLines() string {
	var b strings.Builder
	for i, r := range m.rows {
		switch r.kind {
		case rowHeader:
			b.WriteString(m.st.groupHeader.Render("▸ " + r.label))
		case rowAll:
			// Full-width bar so the highlight reads as a selection, not just text.
			b.WriteString(m.rowStyle(i).Width(sidebarWidth - 2).Render("▸ ALL"))
		case rowStack:
			// Icon shape = kind, icon color = health. Name truncated so a long
			// name can't wrap the row (which used to strand the glyph on its own
			// line). Two cells go to the icon + space; the rest to the name bar.
			icon := m.st.glyphStyle(r.running, r.total).Render(kindGlyph(r.selKind))
			name := truncate(r.label, sidebarWidth-4)
			b.WriteString(icon + " " + m.rowStyle(i).Width(sidebarWidth-4).Render(name))
		}
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

// rowStyle picks a row's style: a bright bar for the selection when the sidebar
// is focused, a dim bar when the selection is retained but focus is in the
// detail pane, and plain otherwise — so the active location is always visible.
func (m Model) rowStyle(i int) lipgloss.Style {
	if i != m.sel {
		return m.st.row
	}
	if m.focus == focusSidebar {
		return m.st.selectedRow
	}
	return m.st.selectedRowDim
}
