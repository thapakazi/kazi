package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
)

// M7 Stats-tab viewer. It mirrors the Logs tab: one live `<runtime> stats`
// stream at a time, opened on entering the tab and torn down on leave, feeding a
// per-container ring buffer the sparklines are drawn from. The engine stays
// stateless — history/aggregation lives here.

// statsSparkW is the fixed sparkline width (glyphs) for the per-service graphs.
const statsSparkW = 8

// statSeries is one container's rolling history plus its latest full sample.
type statSeries struct {
	latest engine.ContainerStats
	cpu    []float64 // CPU% ring (auto-scaled sparkline)
	mem    []float64 // Mem% ring (0..100 sparkline)
}

// appendRing appends v to xs, trimming the oldest so len never exceeds cap
// (cap<=0 ⇒ unbounded, only used defensively).
func appendRing(xs []float64, v float64, cap int) []float64 {
	xs = append(xs, v)
	if cap > 0 && len(xs) > cap {
		xs = xs[len(xs)-cap:]
	}
	return xs
}

// onStatsTab reports whether the Stats tab is the active, focused detail view —
// the only context in which the stats keys (c/z) shadow generic motion.
func (m Model) onStatsTab() bool {
	return m.mode == modeStacks && m.focus == focusDetail && m.tab == tabStats
}

// recordStatSample folds one streamed sample into the ring buffer, keyed by
// container name (the stable join key; docker stats emits short IDs).
func (m *Model) recordStatSample(s engine.StatSample) {
	if m.statsSeries == nil {
		m.statsSeries = map[string]*statSeries{}
	}
	ser := m.statsSeries[s.Name]
	if ser == nil {
		ser = &statSeries{}
		m.statsSeries[s.Name] = ser
	}
	ser.latest = s.ContainerStats
	ser.cpu = appendRing(ser.cpu, s.CPUPercent, m.statsHistory)
	ser.mem = appendRing(ser.mem, s.MemPercent, m.statsHistory)
}

// statsContainersFor returns the container list to render, preferring the fresher
// per-stack status read (states) over the sidebar snapshot — same source as the
// Services tab, so the two stay consistent.
func (m Model) statsContainersFor(s engine.StackInfo) []engine.ContainerInfo {
	if m.statusName == s.Name && len(m.statusInfo.Containers) > 0 {
		return m.statusInfo.Containers
	}
	return s.Containers
}

// desiredStatsTarget reports the (stack, service) the Stats tab should stream for
// the current view, or ("","") when stats shouldn't stream (not the Stats tab,
// ALL/catalog, or an unmanaged loose container). The service filter is sticky
// while the same stack stays selected, mirroring the Logs tab.
func (m Model) desiredStatsTarget() (stack, service string) {
	if m.mode != modeStacks || m.tab != tabStats {
		return "", ""
	}
	r := m.selectedRow()
	if r == nil || r.kind != rowStack || r.stack == nil || r.selKind == selUnmanaged {
		return "", ""
	}
	if r.label == m.statsStack {
		return r.label, m.statsService
	}
	return r.label, ""
}

// statsTargetIDs collects the running container IDs of the selected stack,
// optionally filtered to one service. The engine's StatsStream is scoped to
// these; a service filter narrows it.
func (m Model) statsTargetIDs(service string) []string {
	r := m.selectedRow()
	if r == nil || r.stack == nil {
		return nil
	}
	var ids []string
	for _, c := range m.statsContainersFor(*r.stack) {
		if c.State != "running" || c.ID == "" {
			continue
		}
		if service != "" {
			svc := c.Service
			if svc == "" {
				svc = c.Name
			}
			if svc != service {
				continue
			}
		}
		ids = append(ids, c.ID)
	}
	return ids
}

// statsSyncCmd reconciles the running stats stream with the desired target: a
// no-op when they already match, otherwise it tears the current stream down and
// (if a new target exists) opens a fresh one. Called from navCmd on every
// selection/tab/mode change and each poll tick.
func (m *Model) statsSyncCmd() tea.Cmd {
	want, wantSvc := m.desiredStatsTarget()
	if want == m.statsStack && wantSvc == m.statsService {
		return nil
	}
	m.stopStatsStream()
	if want == "" {
		return nil
	}
	m.statsStack = want
	m.statsService = wantSvc
	m.statsStreaming = true
	return startStatsStreamCmd(m.eng, want, wantSvc, m.statsTargetIDs(wantSvc))
}

// stopStatsStream cancels the active stream (if any) and clears all stats state.
func (m *Model) stopStatsStream() {
	if m.statsCancel != nil {
		m.statsCancel()
	}
	m.statsCancel = nil
	m.statsCh = nil
	m.statsStack = ""
	m.statsService = ""
	m.statsStreaming = false
	m.statsSeries = nil
	m.statsErr = ""
}

// restartStatsStreamCmd tears the current stream down (keeping the stack) and
// re-opens it with the active service filter, resetting the ring buffer.
func (m *Model) restartStatsStreamCmd() tea.Cmd {
	stack, service := m.statsStack, m.statsService
	if stack == "" {
		return nil
	}
	if m.statsCancel != nil {
		m.statsCancel()
	}
	m.statsCancel = nil
	m.statsCh = nil
	m.statsSeries = nil
	m.statsStreaming = true
	m.statsErr = ""
	return startStatsStreamCmd(m.eng, stack, service, m.statsTargetIDs(service))
}

// handleStatsKey resolves a Stats-tab key. It returns ok=false for keys it
// doesn't own so the caller falls through to generic handling.
func (m Model) handleStatsKey(msg tea.KeyMsg) (Model, tea.Cmd, bool) {
	switch msg.String() {
	case "z":
		m.statsFullscreen = !m.statsFullscreen
		return m, nil, true
	case "c":
		sm, cmd := m.openStatsServicePicker()
		return sm, cmd, true
	}
	return m, nil, false
}

// openStatsServicePicker raises the container filter menu for the Stats tab;
// selecting one restarts the stream scoped to it.
func (m Model) openStatsServicePicker() (Model, tea.Cmd) {
	return m.buildServicePicker(modalStatsService, m.statsStack+" — stats: filter container", m.statsService)
}

// statsServiceChoose applies the picked container filter, restarting the stream
// scoped to that service (empty ⇒ all). A no-op when the pick is unchanged.
func (m Model) statsServiceChoose(i int) (tea.Model, tea.Cmd) {
	if i < 0 || i >= len(m.modal.values) {
		m.modal = modalState{}
		return m, nil
	}
	svc := m.modal.values[i]
	m.modal = modalState{}
	if svc == m.statsService {
		return m, nil
	}
	m.statsService = svc
	return m, m.restartStatsStreamCmd()
}

// statsEscape unwinds one level of Stats-tab state (fullscreen → defocus),
// returning true when it consumed the Esc.
func (m *Model) statsEscape() bool {
	switch {
	case m.statsFullscreen:
		m.statsFullscreen = false
		return true
	case m.focus == focusDetail:
		m.focus = focusSidebar
		return true
	}
	return false
}

// statsKeyHints are the contextual keybar actions shown while on the Stats tab.
func statsKeyHints() []keyHint {
	return []keyHint{{"c", "container"}, {"z", "full"}}
}

// renderStats draws the per-service resource view: a CPU%/Mem% sparkline pair per
// running container plus current net/block/PID values. Stopped/exited containers
// show "—" (never zero, which is a real running value); a runtime without JSON
// stats degrades to an unavailable notice.
func (m Model) renderStats(s engine.StackInfo, width int) string {
	if m.statsErr != "" {
		return m.st.tabInactive.Render(m.statsErr)
	}
	containers := m.statsContainersFor(s)
	var rows []engine.ContainerInfo
	for _, c := range containers {
		if m.statsService != "" {
			svc := c.Service
			if svc == "" {
				svc = c.Name
			}
			if svc != m.statsService {
				continue
			}
		}
		rows = append(rows, c)
	}
	if len(rows) == 0 {
		return m.st.tabInactive.Render("no services")
	}

	var b strings.Builder
	if m.statsService != "" {
		b.WriteString(m.st.tabInactive.Render("svc:"+m.statsService) + "\n\n")
	}
	for _, c := range rows {
		svc := c.Service
		if svc == "" {
			svc = c.Name
		}
		if c.State != "running" {
			fmt.Fprintf(&b, "  %-8s —\n", svc)
			continue
		}
		ser := m.statsSeries[c.Name]
		if ser == nil {
			fmt.Fprintf(&b, "  %-8s streaming…\n", svc)
			continue
		}
		st := ser.latest
		fmt.Fprintf(&b, "  %-8s CPU  %s %5.1f%%    Mem  %s  %s / %s (%.0f%%)\n",
			svc, spark(ser.cpu, statsSparkW, 0), st.CPUPercent,
			spark(ser.mem, statsSparkW, 100), st.MemUsage, st.MemLimit, st.MemPercent)
		fmt.Fprintf(&b, "  %-8s net  ↓%s ↑%s    blk  r %s w %s    PIDs %d\n",
			"", st.NetRx, st.NetTx, st.BlockRead, st.BlockWrite, st.PIDs)
	}
	return truncateBlock(b.String(), width)
}

// renderHostOverview draws the ALL overview's host CPU/Memory/Disk graphs plus
// the aggregate container-usage line. Empty until the first host poll lands;
// per-metric degradation (a zero total) shows "—" for just that metric.
func (m Model) renderHostOverview() string {
	if !m.hostHave {
		return ""
	}
	hs := m.hostStats
	var b strings.Builder

	cpuLabel := "CPU"
	if hs.CPUCores > 0 {
		cpuLabel = fmt.Sprintf("CPU (%d cores)", hs.CPUCores)
	}
	fmt.Fprintf(&b, "%-18s %s %.0f%%\n", cpuLabel, spark(m.hostCPUHist, 12, 0), hs.CPUPercent)

	if hs.MemTotal > 0 {
		fmt.Fprintf(&b, "%-18s %s %s/%s\n",
			"Memory ("+fmtBytes(hs.MemTotal)+")", spark(m.hostMemHist, 12, 100),
			fmtBytes(hs.MemUsed), fmtBytes(hs.MemTotal))
	} else {
		fmt.Fprintf(&b, "%-18s —\n", "Memory")
	}

	if hs.DiskTotal > 0 {
		fmt.Fprintf(&b, "%-18s %.0f%% used · %s/%s\n",
			"Disk", pctU(hs.DiskUsed, hs.DiskTotal), fmtBytes(hs.DiskUsed), fmtBytes(hs.DiskTotal))
	} else {
		fmt.Fprintf(&b, "%-18s —\n", "Disk")
	}

	fmt.Fprintf(&b, "\naggregate containers: CPU %.1f%%  Mem %s across %d stacks\n",
		m.aggCPU, fmtBytes(m.aggMem), m.aggStacks)
	return b.String()
}

// renderStatsFullscreen draws the Stats viewer as a centered, bordered popup for
// bigger graphs, reusing the Logs popup geometry (same motion contract). It
// re-renders the tabbed detail body sized to the box interior.
func (m Model) renderStatsFullscreen() string {
	boxW, boxH := m.logFullBoxSize()
	innerW := m.logFullContentWidth()

	var content string
	if r := m.selectedRow(); r != nil && r.stack != nil {
		content = m.renderTabs(*r.stack, innerW)
	} else {
		content = m.renderStats(engine.StackInfo{}, innerW)
	}
	return m.st.logFull.
		Width(boxW - 2*logFullBorder).
		Height(boxH - 2*logFullBorder).
		Render(content)
}

// truncateBlock clips every line of s to width cells.
func truncateBlock(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = truncate(ln, width)
	}
	return strings.Join(lines, "\n")
}

// pctU is a used/total percentage over unsigned byte counts, guarding a zero
// denominator.
func pctU(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

// fmtBytes formats a byte count compactly (base 1024, one decimal, single-letter
// suffix — "36.0G") for the host graphs and aggregate line.
func fmtBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%c", float64(b)/float64(div), "KMGTPE"[exp])
}
