package tui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/thapakazi/kazi/internal/engine"
)

// M5-Log Logs-tab viewer. Everything here is client-side over the in-memory
// logLines ring except tail/since, which restart the engine stream with new
// --tail/--since opts. Search, grouping, copy, and scroll are pure views over
// the bytes already streamed.

// logTailLadder is cycled by `t`; logSinceLadder by `s`. "all" is the sentinel
// that maps to an unbounded stream (no --since; --tail all).
var (
	logTailLadder  = []string{"100", "500", "1000", "all"}
	logSinceLadder = []string{"1m", "5m", "10m", "30m", "1h", "2h", "5h", "all"}
)

// logChromeRows is how many rows the Logs body spends on non-log lines: the tab
// header + its blank (from renderTabs), then the control strip + its blank.
const logChromeRows = 4

const logMatchMarker = "◀ match"

var logMatchStyle = lipgloss.NewStyle().Reverse(true)

// onLogsTab reports whether the Logs tab is the active, focused detail view —
// the only context in which the log keys shadow generic motion.
func (m Model) onLogsTab() bool {
	return m.mode == modeStacks && m.focus == focusDetail && m.tab == tabLogs
}

// nextInLadder returns the entry after cur in ladder, wrapping; the first entry
// when cur isn't found (e.g. an unset default).
func nextInLadder(ladder []string, cur string) string {
	for i, v := range ladder {
		if v == cur {
			return ladder[(i+1)%len(ladder)]
		}
	}
	return ladder[0]
}

// logStreamOpts maps the active tail/since ladder positions to the engine's
// compose flags. "all" since means no --since; tail passes through ("all" is a
// valid --tail value).
func (m Model) logStreamOpts() engine.LogStreamOpts {
	tail := m.logTail
	if tail == "" {
		tail = "500"
	}
	since := m.logSince
	if since == "all" {
		since = ""
	}
	return engine.LogStreamOpts{Tail: tail, Since: since}
}

// logCap is the in-memory ring size for the current tail: the numeric tail, or
// a generous bound for "all". The buffer keeps growing while follow is paused,
// so this caps memory regardless.
func (m Model) logCap() int {
	switch m.logTail {
	case "all":
		return 10000
	case "":
		return 500
	default:
		if n, err := strconv.Atoi(m.logTail); err == nil && n > 0 {
			return n
		}
		return 500
	}
}

// logDisplayLines is what the viewport draws: the raw buffer, or the pattern
// buckets when grouping is on.
func (m Model) logDisplayLines() []string {
	if m.logGrouped {
		return groupedLogLines(m.logLines)
	}
	return m.logLines
}

// logSearchMatches returns the indices in lines that contain query
// (case-insensitive). Pure.
func logSearchMatches(lines []string, query string) []int {
	if query == "" {
		return nil
	}
	q := strings.ToLower(query)
	var out []int
	for i, ln := range lines {
		if strings.Contains(strings.ToLower(ln), q) {
			out = append(out, i)
		}
	}
	return out
}

// logMatchIndices is the current match set over the displayed lines.
func (m Model) logMatchIndices() []int {
	return logSearchMatches(m.logDisplayLines(), m.logSearch)
}

// logViewportHeight is the number of log rows the pane can show — in the
// fullscreen popup that's the box interior; otherwise the detail body height,
// both minus the tab/strip chrome. Scroll math (logMaxTop, logTop) reads this,
// so following and clamping stay correct across the mode toggle.
func (m Model) logViewportHeight() int {
	var h int
	if m.logFullscreen {
		h = m.logFullContentHeight() - logChromeRows
	} else {
		h = m.bodyHeight() - logChromeRows
	}
	if h < 1 {
		h = 1
	}
	return h
}

// Fullscreen popup geometry. logFullMargin is the gap left on every side so the
// box floats over the dashboard rather than blanking it. The rounded border
// adds 1 cell per side; Padding(1,2) adds 1 row / 2 cols of interior padding.
const (
	logFullMarginX = 3
	logFullMarginY = 2
	logFullBorder  = 1
	logFullPadX    = 2
	logFullPadY    = 1
)

// logFullBoxSize is the outer size of the popup (border included), derived from
// the terminal size less the all-sides margin.
func (m Model) logFullBoxSize() (w, h int) {
	w = m.width - 2*logFullMarginX
	h = m.height - 2*logFullMarginY
	if w < 20 {
		w = 20
	}
	if h < 6 {
		h = 6
	}
	return w, h
}

// logFullContentWidth / logFullContentHeight are the interior text dimensions of
// the popup (inside border + padding) that the Logs body renders into.
func (m Model) logFullContentWidth() int {
	w, _ := m.logFullBoxSize()
	return w - 2*logFullBorder - 2*logFullPadX
}

func (m Model) logFullContentHeight() int {
	_, h := m.logFullBoxSize()
	return h - 2*logFullBorder - 2*logFullPadY
}

// renderLogsFullscreen draws the Logs viewer as a centered, bordered popup. It
// reuses the tabbed detail body (so the tab header, stack label, and control
// strip all show) sized to the box interior; overlay() composites it centered,
// which lands it inside the margin on every side.
func (m Model) renderLogsFullscreen() string {
	boxW, boxH := m.logFullBoxSize()
	innerW := m.logFullContentWidth()

	var content string
	if r := m.selectedRow(); r != nil && r.stack != nil {
		content = m.renderTabs(*r.stack, innerW)
	} else {
		content = m.renderLogsPane(innerW)
	}
	return m.st.logFull.
		Width(boxW - 2*logFullBorder).
		Height(boxH - 2*logFullBorder).
		Render(content)
}

// logMaxTop is the largest valid top-line index (bottom-pinned position).
func (m Model) logMaxTop() int {
	t := len(m.logDisplayLines()) - m.logViewportHeight()
	if t < 0 {
		return 0
	}
	return t
}

// logTop resolves the first visible line index: bottom-pinned while following,
// else the clamped scroll offset.
func (m Model) logTop() int {
	if m.logFollow {
		return m.logMaxTop()
	}
	top := m.logScroll
	if maxTop := m.logMaxTop(); top > maxTop {
		top = maxTop
	}
	if top < 0 {
		top = 0
	}
	return top
}

// logVisibleLines is the slice currently on screen (what `y` yanks).
func (m Model) logVisibleLines() []string {
	lines := m.logDisplayLines()
	top := m.logTop()
	end := top + m.logViewportHeight()
	if end > len(lines) {
		end = len(lines)
	}
	if top > len(lines) {
		top = len(lines)
	}
	return lines[top:end]
}

// logScrollBy moves the viewport by delta lines. Scrolling up pauses follow;
// reaching the bottom resumes it (and snaps to latest thereafter).
func (m *Model) logScrollBy(delta int) {
	maxTop := m.logMaxTop()
	top := m.logScroll
	if m.logFollow {
		top = maxTop
	}
	top += delta
	if top < 0 {
		top = 0
	}
	if top >= maxTop {
		top = maxTop
		m.logFollow = true
	} else {
		m.logFollow = false
	}
	m.logScroll = top
}

// logRevealMatch scrolls the current match into view and pauses follow.
func (m *Model) logRevealMatch() {
	idxs := m.logMatchIndices()
	if len(idxs) == 0 {
		return
	}
	m.logMatchCur = ((m.logMatchCur % len(idxs)) + len(idxs)) % len(idxs)
	target := idxs[m.logMatchCur]
	H := m.logViewportHeight()
	top := m.logTop()
	if target < top {
		top = target
	}
	if target >= top+H {
		top = target - H + 1
	}
	if top < 0 {
		top = 0
	}
	m.logScroll = top
	m.logFollow = false
}

// handleLogKey resolves a Logs-tab key. It returns ok=false for keys it doesn't
// own so the caller falls through to generic handling (Tab, R, ?, h/l focus).
func (m Model) handleLogKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "f":
		m.logFollow = !m.logFollow
		if !m.logFollow {
			// Freeze the view where it is (the current bottom) rather than
			// snapping to the top; resuming re-pins to latest.
			m.logScroll = m.logMaxTop()
		}
		return m, nil, true
	case "t":
		m.logTail = nextInLadder(logTailLadder, m.logTail)
		cmd := m.restartLogStreamCmd()
		return m, cmd, true
	case "s":
		m.logSince = nextInLadder(logSinceLadder, m.logSince)
		cmd := m.restartLogStreamCmd()
		return m, cmd, true
	case "/":
		m.logSearching = true
		m.logSearch = ""
		m.logMatchCur = 0
		return m, nil, true
	case "n":
		if len(m.logMatchIndices()) > 0 {
			m.logMatchCur++
			m.logRevealMatch()
		}
		return m, nil, true
	case "N":
		if len(m.logMatchIndices()) > 0 {
			m.logMatchCur--
			m.logRevealMatch()
		}
		return m, nil, true
	case "p":
		m.logGrouped = !m.logGrouped
		m.logScroll = 0
		m.logMatchCur = 0
		m.logFollow = !m.logGrouped // grouped view is static; raw resumes follow
		return m, nil, true
	case "y":
		cmd := m.copyLinesCmd(m.logVisibleLines())
		return m, cmd, true
	case "Y":
		cmd := m.copyLinesCmd(m.logDisplayLines())
		return m, cmd, true
	case "j", "down":
		m.logScrollBy(1)
		return m, nil, true
	case "k", "up":
		m.logScrollBy(-1)
		return m, nil, true
	case "ctrl+d":
		m.logScrollBy(m.logViewportHeight() / 2)
		return m, nil, true
	case "ctrl+u":
		m.logScrollBy(-m.logViewportHeight() / 2)
		return m, nil, true
	case "g":
		m.logScroll = 0
		m.logFollow = false
		return m, nil, true
	case "G":
		m.logFollow = true
		m.logScroll = m.logMaxTop()
		return m, nil, true
	case "z":
		// Toggle the near-fullscreen log popup. Scroll/follow state carries over
		// unchanged; only the viewport height (and thus max-top) differs.
		m.logFullscreen = !m.logFullscreen
		return m, nil, true
	case "c":
		// Open the container filter picker (scopes the stream to one service).
		lm, cmd := m.openLogServicePicker()
		return lm, cmd, true
	}
	return m, nil, false
}

// selectedServiceNames is the sorted, distinct set of services (or container
// names, when a container declares no compose service) for the selected stack.
// It mirrors renderServices' container source so the Logs/Env filter pickers
// match the Services tab. Empty when nothing is loaded yet.
func (m Model) selectedServiceNames() []string {
	r := m.selectedRow()
	if r == nil || r.stack == nil {
		return nil
	}
	containers := r.stack.Containers
	if m.statusName == r.stack.Name && len(m.statusInfo.Containers) > 0 {
		containers = m.statusInfo.Containers
	}
	seen := map[string]bool{}
	var names []string
	for _, c := range containers {
		svc := c.Service
		if svc == "" {
			svc = c.Name
		}
		if svc == "" || seen[svc] {
			continue
		}
		seen[svc] = true
		names = append(names, svc)
	}
	sort.Strings(names)
	return names
}

// buildServicePicker raises a transient container filter menu: "all services"
// plus one row per service, with the cursor pre-parked on the active filter.
// Shared by the Logs (kind=modalLogService) and Env (modalEnvService) tabs; the
// per-kind choose handler applies the pick. current is the active filter value.
func (m Model) buildServicePicker(kind modalKind, prompt, current string) (Model, tea.Cmd) {
	names := m.selectedServiceNames()
	if len(names) == 0 {
		return m, m.setToast("no containers to filter yet")
	}
	opts := []string{"all services"}
	vals := []string{""}
	cursor := 0
	for i, n := range names {
		opts = append(opts, n)
		vals = append(vals, n)
		if n == current {
			cursor = i + 1
		}
	}
	m.modal = modalState{
		active: true, mkind: kind, prompt: prompt,
		options: opts, values: vals, cursor: cursor,
	}
	return m, nil
}

// openLogServicePicker raises the container filter menu for the Logs tab.
// Selecting one restarts the stream scoped to it (logServiceChoose).
func (m Model) openLogServicePicker() (Model, tea.Cmd) {
	return m.buildServicePicker(modalLogService, m.logStack+" — logs: filter container", m.logService)
}

// logServiceChoose applies the picked container filter: it restarts the stream
// scoped to that service (empty ⇒ the combined view), resetting the buffer the
// way tail/since changes do. A no-op when the pick matches the active filter.
func (m Model) logServiceChoose(i int) (tea.Model, tea.Cmd) {
	if i < 0 || i >= len(m.modal.values) {
		m.modal = modalState{}
		return m, nil
	}
	svc := m.modal.values[i]
	m.modal = modalState{}
	if svc == m.logService {
		return m, nil
	}
	m.logService = svc
	return m, m.restartLogStreamCmd()
}

// handleLogSearchKey collects incremental log-search input. Enter locks the
// search (jumping to the first match); Esc abandons it. Matches recompute live.
func (m Model) handleLogSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.logSearching = false
		m.logMatchCur = 0
		m.logRevealMatch()
		return m, nil
	case tea.KeyEsc:
		m.logSearching = false
		m.logSearch = ""
		m.logMatchCur = 0
		return m, nil
	case tea.KeyBackspace:
		if m.logSearch != "" {
			m.logSearch = m.logSearch[:len(m.logSearch)-1]
		}
		m.logMatchCur = 0
		return m, nil
	case tea.KeyRunes, tea.KeySpace:
		if msg.Type == tea.KeySpace {
			m.logSearch += " "
		} else {
			m.logSearch += string(msg.Runes)
		}
		m.logMatchCur = 0
		return m, nil
	}
	return m, nil
}

// logEscape unwinds one level of Logs-tab state (search → grouping → defocus),
// returning true when it consumed the Esc.
func (m *Model) logEscape() bool {
	switch {
	case m.logSearch != "":
		m.logSearch = ""
		m.logMatchCur = 0
		return true
	case m.logFullscreen:
		m.logFullscreen = false
		return true
	case m.logGrouped:
		m.logGrouped = false
		m.logScroll = 0
		m.logFollow = true
		return true
	case m.focus == focusDetail:
		m.focus = focusSidebar
		return true
	}
	return false
}

// restartLogStreamCmd tears the current stream down (keeping the target) and
// re-opens it with the active tail/since opts, resetting the buffer since those
// change what compose replays.
func (m *Model) restartLogStreamCmd() tea.Cmd {
	stack, service := m.logStack, m.logService
	if stack == "" {
		return nil
	}
	if m.logCancel != nil {
		m.logCancel()
	}
	if m.logReader != nil {
		m.logReader.Close()
	}
	m.logCancel = nil
	m.logReader = nil
	m.logScanner = nil
	m.logLines = nil
	m.logScroll = 0
	m.logStreaming = true
	return startLogStreamCmd(m.eng, stack, service, m.logStreamOpts())
}

// logControlStrip is the derived state line under the tab header. Fields
// collapse to nothing at their defaults (no since when "all", no search when
// empty, no group when off).
func (m Model) logControlStrip() string {
	follow := "●"
	if !m.logFollow {
		follow = "‖"
	}
	tail := m.logTail
	if tail == "" {
		tail = "500"
	}
	parts := []string{"follow:" + follow, "tail:" + tail}
	if m.logSince != "" && m.logSince != "all" {
		parts = append(parts, "since:"+m.logSince)
	}
	if m.logService != "" {
		parts = append(parts, "svc:"+m.logService)
	}
	switch {
	case m.logSearching:
		parts = append(parts, "/"+m.logSearch+"▏")
	case m.logSearch != "":
		idxs := m.logMatchIndices()
		cur := 0
		if len(idxs) > 0 {
			cur = ((m.logMatchCur%len(idxs))+len(idxs))%len(idxs) + 1
		}
		parts = append(parts, fmt.Sprintf("/%s (%d/%d)", m.logSearch, cur, len(idxs)))
	}
	if m.logGrouped {
		parts = append(parts, "group:on")
	}
	return strings.Join(parts, "  ")
}

// logKeyHints are the contextual keybar actions shown while on the Logs tab.
func logKeyHints() []keyHint {
	return []keyHint{
		{"f", "follow"}, {"t", "tail"}, {"s", "since"}, {"c", "container"},
		{"/", "search"}, {"n", "next"}, {"p", "group"}, {"z", "full"},
		{"y", "copy"}, {"Y", "copy-all"},
	}
}

// renderLogsPane draws the control strip and the (scrollable) log viewport,
// clipped to width and highlighting search matches. Empty-buffer states show a
// hint under the strip.
func (m Model) renderLogsPane(width int) string {
	strip := m.st.tabInactive.Render(truncate(m.logControlStrip(), width))
	if len(m.logLines) == 0 {
		hint := "no logs — select a running stack"
		if m.logStreaming {
			hint = "streaming logs… (waiting for output)"
		}
		return strip + "\n\n" + m.st.tabInactive.Render(hint)
	}
	lines := m.logDisplayLines()
	top := m.logTop()
	end := top + m.logViewportHeight()
	if end > len(lines) {
		end = len(lines)
	}

	idxs := m.logMatchIndices()
	curAbs := -1
	if len(idxs) > 0 {
		c := ((m.logMatchCur % len(idxs)) + len(idxs)) % len(idxs)
		curAbs = idxs[c]
	}

	var b strings.Builder
	b.WriteString(strip)
	b.WriteString("\n\n")
	for i := top; i < end; i++ {
		if i > top {
			b.WriteByte('\n')
		}
		row := truncate(lines[i], width)
		if m.logSearch != "" {
			row = highlightMatches(row, m.logSearch)
		}
		if i == curAbs {
			row += "  " + logMatchMarker
		}
		b.WriteString(row)
	}
	return b.String()
}

// highlightMatches reverse-videos every case-insensitive occurrence of query in
// line. It runs after truncation, on the raw (unstyled) text.
func highlightMatches(line, query string) string {
	if query == "" {
		return line
	}
	q := strings.ToLower(query)
	var b strings.Builder
	rest := line
	lower := strings.ToLower(line)
	for {
		idx := strings.Index(lower, q)
		if idx < 0 {
			b.WriteString(rest)
			break
		}
		b.WriteString(rest[:idx])
		b.WriteString(logMatchStyle.Render(rest[idx : idx+len(q)]))
		rest = rest[idx+len(q):]
		lower = lower[idx+len(q):]
	}
	return b.String()
}
