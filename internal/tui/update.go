package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
)

// Update dispatches window/tick/data messages and key presses.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		// Polling pauses while a modal is open so state isn't yanked mid-decision;
		// keep the ticker alive so it resumes when the modal closes.
		if m.modal.active {
			return m, tickCmd(m.refresh)
		}
		// Poll: refresh the sidebar + status bar, and the selected stack's detail.
		return m, tea.Batch(
			snapshotCmd(m.eng),
			statusbarCmd(m.eng),
			m.detailReadCmd(),
			tickCmd(m.refresh),
		)

	case snapshotMsg:
		m.stale = false
		m.err = nil
		prev := m.selectedName()
		// A freshly created/tried stack takes selection focus on arrival.
		if m.pendingSelect != "" {
			prev = m.pendingSelect
			m.pendingSelect = ""
		}
		m.rows = buildRows(msg.stacks, msg.loose, m.filter)
		m.restoreSelection(prev)
		// The watched ephemeral stack is gone (kept/gc'd/reaped) → stop watching.
		if m.watchStack != "" && m.rowFor(m.watchStack) == nil {
			m.watchStack = ""
		}
		return m, m.navCmd()

	case statusbarMsg:
		m.runtimeName = msg.runtime
		m.proxyUp = msg.proxyUp
		m.gcCount = msg.gcCount
		return m, nil

	case statusMsg:
		m.statusName = msg.stack
		m.statusInfo = msg.info
		return m, nil

	case urlsMsg:
		m.endpointsFor = msg.stack
		m.endpoints = msg.endpoints
		return m, nil

	case describeMsg:
		m.detailFor = msg.stack
		m.detail = msg.detail
		return m, nil

	case templatesMsg:
		m.templates = msg.templates
		return m, nil

	case logStreamMsg:
		// If we navigated away before the stream opened, discard it.
		if msg.stack != m.logStack || msg.service != m.logService {
			msg.cancel()
			msg.reader.Close()
			return m, nil
		}
		m.logReader = msg.reader
		m.logCancel = msg.cancel
		m.logScanner = msg.scanner
		return m, readLogCmd(msg.scanner, msg.stack)

	case logLineMsg:
		if msg.stack != m.logStack {
			return m, nil // stale stream
		}
		m.logLines = append(m.logLines, msg.line)
		if cap := m.logCap(); len(m.logLines) > cap {
			m.logLines = m.logLines[len(m.logLines)-cap:]
		}
		return m, readLogCmd(m.logScanner, msg.stack)

	case logDoneMsg:
		if msg.stack == m.logStack {
			m.logStreaming = false
		}
		return m, nil

	case actionStreamMsg:
		// A captured lifecycle verb started: open the Action panel and pump lines.
		m.actionRunning = true
		m.actionOpen = true
		m.actionTitle = msg.action + " " + msg.stack
		m.actionLines = nil
		m.actionScroll = 0
		m.actionScanner = msg.scanner
		m.actionErrc = msg.errc
		m.actionVerb, m.actionName = msg.action, msg.stack
		return m, readActionCmd(msg.scanner, msg.errc, msg.action, msg.stack)

	case actionLineMsg:
		m.actionLines = append(m.actionLines, msg.line)
		if len(m.actionLines) > actionCap {
			m.actionLines = m.actionLines[len(m.actionLines)-actionCap:]
		}
		return m, readActionCmd(m.actionScanner, m.actionErrc, m.actionVerb, m.actionName)

	case actionHistoryMsg:
		// Show persisted history in the panel until an action runs this session.
		if len(msg.lines) > 0 && !m.actionRunning && m.actionTitle == "" {
			m.actionLines = msg.lines
			m.actionTitle = "recent actions"
		}
		return m, nil

	case actionDoneMsg:
		m.actionRunning = false
		if msg.err != nil {
			m.actionTitle = msg.action + " " + msg.stack + " ✗"
			return m, tea.Batch(
				m.setToast(msg.action+" "+msg.stack+": "+msg.err.Error()),
				appendActionLogCmd(msg.action+" "+msg.stack+" ✗", m.actionLines),
			)
		}
		// keep/gc reclaim the watched ephemeral stack; stop watching it.
		if (msg.action == "keep" || msg.action == "gc") && msg.stack == m.watchStack {
			m.watchStack = ""
		}
		// Success: banner it and refresh so the change (e.g. a gone stack) shows.
		// delete/keep/gc aren't streamed lifecycle verbs, so they don't own the
		// Action panel title.
		if msg.action != "delete" && msg.action != "keep" && msg.action != "gc" {
			m.actionTitle = msg.action + " " + msg.stack + " ✓"
		}
		return m, tea.Batch(
			m.setToast(msg.action+" "+msg.stack+" ✓"),
			snapshotCmd(m.eng), statusbarCmd(m.eng),
			appendActionLogCmd(msg.action+" "+msg.stack+" ✓", m.actionLines),
		)

	case createDoneMsg:
		// Create form (any source): on error keep the form open with an inline
		// message; on success close it and select the new stack next snapshot.
		if msg.err != nil {
			m.form.err = msg.err.Error()
			return m, nil
		}
		m.form = formState{}
		m.mode = modeStacks
		m.pendingSelect = msg.name
		return m, tea.Batch(m.setToast("created "+msg.name+" ✓"), snapshotCmd(m.eng))

	case tryDoneMsg:
		// Try form: close it, then focus + watch the new ephemeral stack (or toast
		// the launch failure — the stack, if any, is gc-recoverable).
		m.form = formState{}
		if msg.err != nil {
			return m, m.setToast("try failed: " + msg.err.Error())
		}
		m.mode = modeStacks
		m.pendingSelect = msg.name
		m.watchStack = msg.name
		return m, tea.Batch(m.setToast("trying "+msg.name+" — k:keep · g:gc"), snapshotCmd(m.eng))

	case routeDoneMsg:
		if msg.count == 0 {
			return m, m.setToast("no published ports to route for " + msg.stack)
		}
		// Refresh the detail read so the new routes show in the URLs tab at once.
		return m, tea.Batch(
			m.setToast(fmt.Sprintf("routed %d port(s) from %s — see URLs", msg.count, msg.stack)),
			m.detailReadCmd(),
		)

	case tryValuesMsg:
		// While the create form is open, template values feed its adaptive fields;
		// otherwise they open the standalone Catalog try form.
		if m.form.active && m.form.kind == formCreate {
			m.form.tmplCache[msg.tmpl] = msg.values
			return m, m.rebuildCreateFields()
		}
		m.openTryForm(msg.tmpl, msg.values)
		return m, nil

	case editTargetsMsg:
		if msg.err != nil {
			return m, m.setToast("edit: " + msg.err.Error())
		}
		m.editStack = msg.stack
		m.editTargets = msg.targets
		switch len(msg.targets) {
		case 0:
			return m, m.setToast("edit: nothing to edit for " + msg.stack)
		case 1:
			return m.beginEdit(msg.stack, msg.targets[0])
		default:
			opts := make([]string, len(msg.targets))
			vals := make([]string, len(msg.targets))
			for i, t := range msg.targets {
				opts[i] = fmt.Sprintf("%-9s %s", t.Kind, t.Path)
				vals[i] = t.Kind
			}
			m.modal = modalState{active: true, mkind: modalEditPick,
				prompt: "edit which file for " + msg.stack + "?", options: opts, values: vals}
			return m, nil
		}

	case editorReturnedMsg:
		// A non-zero editor exit is treated as an abort: restore and write nothing.
		if msg.err != nil {
			m.restoreEdit()
			path := m.editTarget.Path
			m.clearEdit()
			return m, m.setToast("edit aborted (editor error); " + path + " restored")
		}
		return m, editValidateCmd(m.editTarget)

	case editValidatedMsg:
		if msg.err != nil {
			// Invalid save → re-edit (y) or discard (n, restores original).
			m.modal = modalState{active: true, mkind: modalConfirm, action: actEditRetry, stack: m.editStack,
				prompt: "invalid: " + msg.err.Error() + "\n\nre-edit? (y re-opens · n discards changes)"}
			return m, nil
		}
		stack, kind := m.editStack, m.editTarget.Kind
		running := false
		if r := m.rowFor(stack); r != nil {
			running = r.running > 0
		}
		m.clearEdit()
		cmds := []tea.Cmd{m.setToast("saved " + kind + " for " + stack), snapshotCmd(m.eng)}
		// Running stack: offer a restart to apply (kazi never silently recreates).
		if running {
			m.modal = modalState{active: true, mkind: modalConfirm, action: actRestart, stack: stack,
				prompt: fmt.Sprintf("%q is running — restart now to apply the edit?", stack)}
		}
		return m, tea.Batch(cmds...)

	case openResolvedMsg:
		switch len(msg.choices) {
		case 0:
			return m, m.setToast("no URL for " + msg.stack)
		case 1:
			return m, openCmd(msg.choices[0].url)
		default:
			opts := make([]string, len(msg.choices))
			vals := make([]string, len(msg.choices))
			for i, c := range msg.choices {
				opts[i], vals[i] = c.label, c.url
			}
			m.modal = modalState{active: true, mkind: modalPicker,
				prompt: "open which URL for " + msg.stack + "?", options: opts, values: vals}
			return m, nil
		}

	case openedMsg:
		if msg.err != nil {
			return m, m.setToast("open failed: " + msg.err.Error())
		}
		return m, m.setToast("opened " + msg.url)

	case toastClearMsg:
		if msg.seq == m.toastSeq {
			m.toast = ""
		}
		return m, nil

	case errMsg:
		// Keep the last good frame; flag staleness. Never blank the screen.
		m.err = msg.err
		m.stale = true
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// handleKey routes a key press: filter-input mode first, then global keys,
// then motion.
func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// A confirm modal captures keys until it resolves.
	if m.modal.active {
		return m.handleModalKey(msg)
	}
	// An input form (create n / try t) captures keys until it submits or aborts.
	if m.form.active {
		return m.handleFormKey(msg)
	}
	// The Logs-tab search is its own incremental input mode (distinct from the
	// sidebar filter).
	if m.logSearching {
		return m.handleLogSearchKey(msg)
	}
	if m.filtering {
		return m.handleFilterKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Quit):
		m.stopLogStream()
		return m, tea.Quit
	case key.Matches(msg, m.keys.Help):
		m.help = !m.help
		return m, nil
	case key.Matches(msg, m.keys.Escape):
		if m.help {
			m.help = false
			return m, nil
		}
		// On the Logs tab, Esc unwinds search → grouping → defocus first.
		if m.onLogsTab() && m.logEscape() {
			return m, nil
		}
		if m.filter != "" {
			m.filter = ""
			return m, snapshotCmd(m.eng)
		}
		return m, nil
	}

	// The help overlay swallows motion until dismissed.
	if m.help {
		return m, nil
	}

	// On the Logs tab (detail focused) the log keys shadow generic motion.
	if m.onLogsTab() {
		if lm, cmd, ok := m.handleLogKey(msg); ok {
			return lm, cmd
		}
	}

	switch {
	case key.Matches(msg, m.keys.Mode):
		m.toggleMode()
		return m, m.navCmd()
	case key.Matches(msg, m.keys.Mode1):
		m.mode = modeStacks
		return m, m.navCmd()
	case key.Matches(msg, m.keys.Mode2):
		m.mode = modeCatalog
		return m, m.navCmd()
	case key.Matches(msg, m.keys.Filter):
		m.filtering = true
		return m, nil
	case key.Matches(msg, m.keys.Refresh):
		return m, tea.Batch(snapshotCmd(m.eng), statusbarCmd(m.eng))
	}

	// Toggle the Action panel (collapsible) when there's action output. Bound to
	// ` so a stays free for a:adopt.
	if key.Matches(msg, m.keys.Actions) && m.actionTitle != "" {
		m.actionOpen = !m.actionOpen
		return m, nil
	}
	// Scroll the expanded Action panel's history with PgUp/PgDn.
	if m.actionOpen && m.actionTitle != "" {
		switch msg.Type {
		case tea.KeyPgUp:
			m.scrollAction(m.actionRows())
			return m, nil
		case tea.KeyPgDown:
			m.scrollAction(-m.actionRows())
			return m, nil
		}
	}

	// Contextual action keys (open a guarded modal for the selection).
	if cmd, ok := m.handleActionKey(msg); ok {
		return m, cmd
	}

	// In Catalog mode the motion keys move the template cursor (the sidebar
	// isn't the focus there) so t:try acts on the highlighted template.
	if m.mode == modeCatalog {
		if cmd, ok := m.handleCatalogMotion(msg); ok {
			return m, cmd
		}
	}

	// Motion.
	switch {
	case key.Matches(msg, m.keys.FocusL):
		m.focus = focusSidebar
		return m, nil
	case key.Matches(msg, m.keys.FocusR):
		m.focus = focusDetail
		return m, nil
	case key.Matches(msg, m.keys.Down):
		m.moveSel(1)
		return m, m.navCmd()
	case key.Matches(msg, m.keys.Up):
		m.moveSel(-1)
		return m, m.navCmd()
	case key.Matches(msg, m.keys.Top):
		m.jumpSel(true)
		return m, m.navCmd()
	case key.Matches(msg, m.keys.Bottom):
		m.jumpSel(false)
		return m, m.navCmd()
	case key.Matches(msg, m.keys.HalfDown):
		m.moveSel(5)
		return m, m.navCmd()
	case key.Matches(msg, m.keys.HalfUp):
		m.moveSel(-5)
		return m, m.navCmd()
	case key.Matches(msg, m.keys.TabPrev):
		// Switching tabs enters the stack view so the tab's keys (e.g. log
		// follow/scroll) become active without a separate focus step.
		m.focus = focusDetail
		m.cycleTab(-1)
		return m, m.navCmd()
	case key.Matches(msg, m.keys.TabNext):
		m.focus = focusDetail
		m.cycleTab(1)
		return m, m.navCmd()
	case key.Matches(msg, m.keys.Enter):
		m.focus = focusDetail
		return m, m.navCmd()
	}
	return m, nil
}

// handleFilterKey collects filter input; Enter/Esc exit filter-input mode.
func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		m.filtering = false
		return m, snapshotCmd(m.eng)
	case tea.KeyEsc:
		m.filtering = false
		m.filter = ""
		return m, snapshotCmd(m.eng)
	case tea.KeyBackspace:
		if m.filter != "" {
			m.filter = m.filter[:len(m.filter)-1]
		}
		return m, snapshotCmd(m.eng)
	case tea.KeyRunes:
		m.filter += string(msg.Runes)
		return m, snapshotCmd(m.eng)
	}
	return m, nil
}

// toggleMode flips between Stacks and Catalog.
func (m *Model) toggleMode() {
	if m.mode == modeStacks {
		m.mode = modeCatalog
	} else {
		m.mode = modeStacks
	}
}

// cycleTab moves the active detail tab by delta, wrapping.
func (m *Model) cycleTab(delta int) {
	n := len(tabNames)
	m.tab = detailTab((int(m.tab) + delta + n) % n)
}

// handleCatalogMotion moves the template cursor for Catalog-mode motion keys,
// returning (nil, true) when it owns the key so the sidebar isn't moved too.
func (m *Model) handleCatalogMotion(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.Down):
		m.moveCat(1)
		return nil, true
	case key.Matches(msg, m.keys.Up):
		m.moveCat(-1)
		return nil, true
	case key.Matches(msg, m.keys.HalfDown):
		m.moveCat(5)
		return nil, true
	case key.Matches(msg, m.keys.HalfUp):
		m.moveCat(-5)
		return nil, true
	case key.Matches(msg, m.keys.Top):
		m.catSel = 0
		return nil, true
	case key.Matches(msg, m.keys.Bottom):
		if len(m.templates) > 0 {
			m.catSel = len(m.templates) - 1
		}
		return nil, true
	}
	return nil, false
}

// moveCat shifts the catalog cursor by delta, clamped to the template range.
func (m *Model) moveCat(delta int) {
	m.catSel += delta
	if m.catSel < 0 {
		m.catSel = 0
	}
	if m.catSel >= len(m.templates) {
		m.catSel = len(m.templates) - 1
	}
	if m.catSel < 0 {
		m.catSel = 0
	}
}

// moveSel moves the sidebar selection by delta, skipping non-selectable header
// rows and clamping to the row range.
func (m *Model) moveSel(delta int) {
	if len(m.rows) == 0 {
		return
	}
	step := 1
	if delta < 0 {
		step = -1
		delta = -delta
	}
	i := m.sel
	for n := 0; n < delta; n++ {
		next := m.nextSelectable(i, step)
		if next == i {
			break
		}
		i = next
	}
	m.sel = i
}

// nextSelectable returns the next selectable row index from i in the given
// direction, or i if none.
func (m *Model) nextSelectable(i, step int) int {
	for j := i + step; j >= 0 && j < len(m.rows); j += step {
		if m.rows[j].kind != rowHeader {
			return j
		}
	}
	return i
}

// jumpSel jumps selection to the first/last selectable row.
func (m *Model) jumpSel(top bool) {
	if len(m.rows) == 0 {
		return
	}
	if top {
		m.sel = firstSelectable(m.rows)
		return
	}
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.rows[i].kind != rowHeader {
			m.sel = i
			return
		}
	}
}

// selectedName returns the label of the selected stack row (for stable
// selection across refreshes), or "" for ALL / none.
func (m Model) selectedName() string {
	r := m.selectedRow()
	if r == nil || r.kind != rowStack {
		return ""
	}
	return r.label
}

// restoreSelection re-points sel at the previously selected stack by name after
// a rebuild, clamping to a valid selectable row otherwise.
func (m *Model) restoreSelection(name string) {
	if name != "" {
		for i, r := range m.rows {
			if r.kind == rowStack && r.label == name {
				m.sel = i
				return
			}
		}
	}
	if m.sel < 0 || m.sel >= len(m.rows) || m.rows[m.sel].kind == rowHeader {
		m.sel = firstSelectable(m.rows)
	}
}

// detailReadCmd issues the engine read the active detail view needs for the
// selected stack; nothing for ALL / catalog. Returns nil when no read applies.
func (m Model) detailReadCmd() tea.Cmd {
	if m.mode != modeStacks {
		return nil
	}
	r := m.selectedRow()
	if r == nil || r.kind != rowStack || r.stack == nil {
		return nil
	}
	// Unmanaged loose rows aren't resolvable stacks; skip status/urls reads.
	if r.selKind == selUnmanaged {
		return nil
	}
	name := r.label
	switch m.tab {
	case tabURLs:
		return urlsCmd(m.eng, name)
	case tabConfig:
		return describeCmd(m.eng, name)
	case tabLogs:
		// The Logs tab streams (logSyncCmd); no one-shot read.
		return nil
	default:
		return statusCmd(m.eng, name)
	}
}

// navCmd is issued after any change to the selection, tab, or mode: it batches
// the one-shot detail read with a log-stream (re)sync so the Logs tab follows
// whatever is now selected.
func (m *Model) navCmd() tea.Cmd {
	return tea.Batch(m.detailReadCmd(), m.logSyncCmd())
}

// desiredLogTarget reports the (stack, service) the Logs tab should be streaming
// for the current view, or ("","") when logs shouldn't stream (not the Logs
// tab, ALL/catalog, or an unmanaged loose container that isn't a resolvable
// stack).
func (m Model) desiredLogTarget() (stack, service string) {
	if m.mode != modeStacks || m.tab != tabLogs {
		return "", ""
	}
	r := m.selectedRow()
	if r == nil || r.kind != rowStack || r.stack == nil || r.selKind == selUnmanaged {
		return "", ""
	}
	return r.label, ""
}

// logSyncCmd reconciles the running stream with the desired target: a no-op when
// they already match, otherwise it tears the current stream down and (if a new
// target exists) marks intent and returns the start command. The resulting
// logStreamMsg carries the reader/cancel this model then owns.
func (m *Model) logSyncCmd() tea.Cmd {
	want, wantSvc := m.desiredLogTarget()
	if want == m.logStack && wantSvc == m.logService {
		return nil
	}
	m.stopLogStream()
	if want == "" {
		return nil
	}
	m.logStack = want
	m.logService = wantSvc
	m.logStreaming = true
	return startLogStreamCmd(m.eng, want, wantSvc, m.logStreamOpts())
}

// stopLogStream cancels the active stream (if any) and clears all log state.
func (m *Model) stopLogStream() {
	if m.logCancel != nil {
		m.logCancel()
	}
	if m.logReader != nil {
		m.logReader.Close()
	}
	m.logCancel = nil
	m.logReader = nil
	m.logScanner = nil
	m.logStack = ""
	m.logService = ""
	m.logLines = nil
	m.logStreaming = false
}
