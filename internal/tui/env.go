package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Env tab. A per-container view of each container's `.Config.Env`, fetched once
// per stack (env is fixed at container creation) and cached in m.env. The `c`
// container filter mirrors the Logs tab, but here it's a pure client-side view
// over the cached data — no stream to restart.

// envChromeRows matches logChromeRows: tab header + blank + control strip +
// blank sit above the scrollable body.
const envChromeRows = 4

// onEnvTab reports whether the Env tab is the active, focused detail view — the
// only context in which the env keys shadow generic motion.
func (m Model) onEnvTab() bool {
	return m.mode == modeStacks && m.focus == focusDetail && m.tab == tabEnv
}

// envLine is one rendered row: a container header or one KEY=value (or a
// blank/notice). header rows are styled distinctly.
type envLine struct {
	text   string
	header bool
}

// envLines flattens the cached env into display rows, honoring the container
// filter: a header per container, then its sorted KEY=value lines (or a notice
// for an empty/errored container), with a blank separator between containers.
func (m Model) envLines() []envLine {
	var out []envLine
	for _, ce := range m.env {
		if m.envService != "" && ce.Service != m.envService {
			continue
		}
		title := ce.Service
		if ce.Name != "" && ce.Name != ce.Service {
			title += "  (" + ce.Name + ")"
		}
		out = append(out, envLine{text: title, header: true})
		switch {
		case ce.Err != "":
			out = append(out, envLine{text: "  ! " + ce.Err})
		case len(ce.Env) == 0:
			out = append(out, envLine{text: "  (no environment)"})
		default:
			for _, kv := range ce.Env {
				out = append(out, envLine{text: "  " + kv})
			}
		}
		out = append(out, envLine{text: ""})
	}
	return out
}

// envCopyLines is the flat KEY=value list (with container headers) that y yanks.
func (m Model) envCopyLines() []string {
	lines := m.envLines()
	out := make([]string, 0, len(lines))
	for _, l := range lines {
		out = append(out, l.text)
	}
	return out
}

// envMatchIndices is the current search's match set over the displayed rows.
func (m Model) envMatchIndices() []int {
	if m.envSearch == "" {
		return nil
	}
	lines := m.envLines()
	texts := make([]string, len(lines))
	for i, l := range lines {
		texts[i] = l.text
	}
	return logSearchMatches(texts, m.envSearch)
}

// envRevealMatch scrolls the current match into view.
func (m *Model) envRevealMatch() {
	idxs := m.envMatchIndices()
	if len(idxs) == 0 {
		return
	}
	m.envMatchCur = ((m.envMatchCur % len(idxs)) + len(idxs)) % len(idxs)
	target := idxs[m.envMatchCur]
	H := m.envViewportHeight()
	top := m.envScroll
	if maxTop := m.envMaxTop(); top > maxTop {
		top = maxTop
	}
	if target < top {
		top = target
	}
	if target >= top+H {
		top = target - H + 1
	}
	if top < 0 {
		top = 0
	}
	m.envScroll = top
}

// handleEnvSearchKey collects incremental env-search input. Enter locks the
// search (jumping to the first match); Esc abandons it. Matches recompute live.
func (m Model) handleEnvSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.envSearching = false
		m.envMatchCur = 0
		m.envRevealMatch()
		return m, nil
	case tea.KeyEsc:
		m.envSearching = false
		m.envSearch = ""
		m.envMatchCur = 0
		return m, nil
	case tea.KeyBackspace:
		if m.envSearch != "" {
			m.envSearch = m.envSearch[:len(m.envSearch)-1]
		}
		m.envMatchCur = 0
		return m, nil
	case tea.KeyRunes, tea.KeySpace:
		if msg.Type == tea.KeySpace {
			m.envSearch += " "
		} else {
			m.envSearch += string(msg.Runes)
		}
		m.envMatchCur = 0
		return m, nil
	}
	return m, nil
}

// envViewportHeight is the number of env rows the pane can show.
func (m Model) envViewportHeight() int {
	h := m.bodyHeight() - envChromeRows
	if h < 1 {
		h = 1
	}
	return h
}

// envMaxTop is the largest valid top-line index (bottom of the scroll range).
func (m Model) envMaxTop() int {
	t := len(m.envLines()) - m.envViewportHeight()
	if t < 0 {
		return 0
	}
	return t
}

// envScrollBy moves the viewport by delta rows, clamped to [0, envMaxTop].
func (m *Model) envScrollBy(delta int) {
	top := m.envScroll + delta
	if maxTop := m.envMaxTop(); top > maxTop {
		top = maxTop
	}
	if top < 0 {
		top = 0
	}
	m.envScroll = top
}

// handleEnvKey resolves an Env-tab key. Returns ok=false for keys it doesn't own
// so the caller falls through to generic handling (Tab, R, ?, h/l focus).
func (m Model) handleEnvKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "c":
		em, cmd := m.buildServicePicker(modalEnvService,
			m.envFor+" — env: filter container", m.envService)
		return em, cmd, true
	case "/":
		m.envSearching = true
		m.envSearch = ""
		m.envMatchCur = 0
		return m, nil, true
	case "n":
		if len(m.envMatchIndices()) > 0 {
			m.envMatchCur++
			m.envRevealMatch()
		}
		return m, nil, true
	case "N":
		if len(m.envMatchIndices()) > 0 {
			m.envMatchCur--
			m.envRevealMatch()
		}
		return m, nil, true
	case "y":
		cmd := m.copyLinesCmd(m.envCopyLines())
		return m, cmd, true
	case "j", "down":
		m.envScrollBy(1)
		return m, nil, true
	case "k", "up":
		m.envScrollBy(-1)
		return m, nil, true
	case "ctrl+d":
		m.envScrollBy(m.envViewportHeight() / 2)
		return m, nil, true
	case "ctrl+u":
		m.envScrollBy(-m.envViewportHeight() / 2)
		return m, nil, true
	case "g":
		m.envScroll = 0
		return m, nil, true
	case "G":
		m.envScroll = m.envMaxTop()
		return m, nil, true
	}
	return m, nil, false
}

// envEscape unwinds one level of Env-tab state (filter → defocus), returning
// true when it consumed the Esc.
func (m *Model) envEscape() bool {
	switch {
	case m.envSearch != "":
		m.envSearch = ""
		m.envMatchCur = 0
		return true
	case m.envService != "":
		m.envService = ""
		m.envScroll = 0
		return true
	case m.focus == focusDetail:
		m.focus = focusSidebar
		return true
	}
	return false
}

// envServiceChoose applies the picked container filter (empty ⇒ all). Pure
// client-side: it just narrows what envLines renders and resets the scroll.
func (m Model) envServiceChoose(i int) (tea.Model, tea.Cmd) {
	if i < 0 || i >= len(m.modal.values) {
		m.modal = modalState{}
		return m, nil
	}
	svc := m.modal.values[i]
	m.modal = modalState{}
	if svc != m.envService {
		m.envService = svc
		m.envScroll = 0
		m.envMatchCur = 0 // match set changes with the filtered rows
	}
	return m, nil
}

// envControlStrip is the derived state line under the tab header: the (filtered)
// container count plus the active container filter.
func (m Model) envControlStrip() string {
	n := 0
	for _, ce := range m.env {
		if m.envService != "" && ce.Service != m.envService {
			continue
		}
		n++
	}
	parts := []string{fmt.Sprintf("containers:%d", n)}
	if m.envService != "" {
		parts = append(parts, "svc:"+m.envService)
	}
	switch {
	case m.envSearching:
		parts = append(parts, "/"+m.envSearch+"▏")
	case m.envSearch != "":
		idxs := m.envMatchIndices()
		cur := 0
		if len(idxs) > 0 {
			cur = ((m.envMatchCur%len(idxs))+len(idxs))%len(idxs) + 1
		}
		parts = append(parts, fmt.Sprintf("/%s (%d/%d)", m.envSearch, cur, len(idxs)))
	}
	return strings.Join(parts, "  ")
}

// envKeyHints are the contextual keybar actions shown while on the Env tab.
func envKeyHints() []keyHint {
	return []keyHint{
		{"c", "container"}, {"/", "search"}, {"n", "next"},
		{"j/k", "scroll"}, {"y", "copy-all"},
	}
}

// renderEnv draws the control strip and the scrollable, filtered env viewport.
func (m Model) renderEnv(width int) string {
	strip := m.st.tabInactive.Render(truncate(m.envControlStrip(), width))
	if m.envFor != m.selectedName() {
		return strip + "\n\n" + m.st.tabInactive.Render("reading environment…")
	}
	lines := m.envLines()
	if len(lines) == 0 {
		return strip + "\n\n" + m.st.tabInactive.Render("no containers — start the stack to inspect env")
	}
	top := m.envScroll
	if maxTop := m.envMaxTop(); top > maxTop {
		top = maxTop
	}
	end := top + m.envViewportHeight()
	if end > len(lines) {
		end = len(lines)
	}

	idxs := m.envMatchIndices()
	curAbs := -1
	if len(idxs) > 0 {
		c := ((m.envMatchCur % len(idxs)) + len(idxs)) % len(idxs)
		curAbs = idxs[c]
	}

	var b strings.Builder
	b.WriteString(strip)
	b.WriteString("\n\n")
	for i := top; i < end; i++ {
		if i > top {
			b.WriteByte('\n')
		}
		row := truncate(lines[i].text, width)
		switch {
		case lines[i].header:
			row = m.st.groupHeader.Render(row)
		case m.envSearch != "":
			row = highlightMatches(row, m.envSearch)
		}
		if i == curAbs {
			row += "  " + logMatchMarker
		}
		b.WriteString(row)
	}
	return b.String()
}
