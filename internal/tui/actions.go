package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// toastTTL is how long a result banner lingers before auto-clearing.
const toastTTL = 3 * time.Second

// handleActionKey opens a guarded modal for a contextual action key. It returns
// (cmd, true) when the key was an action it owns, else (nil, false) so the
// caller falls through to motion. Only x:delete is wired today; it applies to
// registered stacks (which have a manifest to deregister).
func (m *Model) handleActionKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "x":
		sel := m.currentSelection()
		if sel.kind != selStack {
			return nil, false // discovered/unmanaged/system: not deletable here
		}
		r := m.selectedRow()
		if r == nil || r.kind != rowStack {
			return nil, false
		}
		m.modal = modalState{
			active: true,
			mkind:  modalConfirm,
			action: actDelete,
			stack:  r.label,
			prompt: fmt.Sprintf("delete stack %q?  (deregisters; containers untouched)", r.label),
		}
		return nil, true
	case "o":
		// Open menu (transient): b → the stack's URL in the browser, e → its
		// config/project in $EDITOR (detached). Offered for registered stacks
		// (always — editing needs no running container) and for running
		// discovered stacks (browser only; they have no manifest to edit).
		sel := m.currentSelection()
		r := m.selectedRow()
		if r == nil || r.kind != rowStack {
			return nil, false
		}
		switch sel.kind {
		case selStack:
			// editable always; browser offered inside the menu when running.
		case selDiscovered:
			if !sel.running {
				return nil, false // nothing to open: no URL, no manifest
			}
		default:
			return nil, false
		}
		m.modal = modalState{active: true, mkind: modalOpenChoose, stack: r.label,
			prompt: "› " + r.label + " — open"}
		return nil, true
	case "s":
		// Quick-actions menu for the selected stack. The system stack (kazi-proxy)
		// is included so it can be started/restarted; its menu omits open/route
		// and never offers delete.
		sel := m.currentSelection()
		if sel.kind != selStack && sel.kind != selDiscovered && sel.kind != selSystem {
			return nil, false
		}
		r := m.selectedRow()
		if r == nil || r.kind != rowStack {
			return nil, false
		}
		m.openStackMenu(r)
		return nil, true
	case "n":
		// New stack: open the transient source chooser (c/t/i) — a Stacks-mode
		// front door over add / try / run.
		if m.mode != modeStacks {
			return nil, false
		}
		m.modal = modalState{active: true, mkind: modalSourceChoose, prompt: "new stack — source"}
		return nil, true
	case "d":
		// Remove/teardown transient. Registered/discovered stacks → down & remove
		// (+ deregister for registered); unmanaged loose containers → docker rm -f.
		// The system stack is protected.
		sel := m.currentSelection()
		if sel.kind != selStack && sel.kind != selDiscovered && sel.kind != selUnmanaged {
			return nil, false
		}
		r := m.selectedRow()
		if r == nil || r.kind != rowStack {
			return nil, false
		}
		m.modal = modalState{active: true, mkind: modalRemoveChoose, stack: r.label, prompt: r.label + " — remove"}
		return nil, true
	case "a":
		// Adopt an unmanaged loose container into a kazi stack named after it.
		if m.currentSelection().kind != selUnmanaged {
			return nil, false
		}
		r := m.selectedRow()
		if r == nil || r.kind != rowStack {
			return nil, false
		}
		return adoptCmd(m.eng, r.label), true
	case "t":
		// Try a catalog template: collect values first, then launch `try -d`.
		if m.mode != modeCatalog || m.catSel < 0 || m.catSel >= len(m.templates) {
			return nil, false
		}
		return tryValuesCmd(m.eng, m.templates[m.catSel].Name), true
	case "k":
		// Keep the watched ephemeral stack (guarded). k is Up-motion otherwise.
		if !m.isWatched() {
			return nil, false
		}
		m.modal = modalState{active: true, mkind: modalConfirm, action: actKeep, stack: m.watchStack,
			prompt: fmt.Sprintf("keep %q?  (promote to a persistent stack; containers untouched)", m.watchStack)}
		return nil, true
	case "g":
		// GC-reclaim the watched ephemeral stack (guarded). g is top-motion otherwise.
		if !m.isWatched() {
			return nil, false
		}
		m.modal = modalState{active: true, mkind: modalConfirm, action: actGc, stack: m.watchStack,
			prompt: fmt.Sprintf("gc %q?  (tear down containers and remove the ephemeral stack)", m.watchStack)}
		return nil, true
	}
	return nil, false
}

// isWatched reports whether the selection is the ephemeral stack just launched
// via the try form — the context in which k:keep / g:gc are offered.
func (m Model) isWatched() bool {
	return m.watchStack != "" && m.selectedName() == m.watchStack
}

// menuItem is one row of the stack quick-actions menu: a dispatch token and a
// description.
type menuItem struct {
	token string
	desc  string
}

// stackMenuItems lists the operations offered for a selected stack, tailored to
// its running state, whether it's registered (deletable), and whether it's the
// protected system stack (kazi-proxy). The system stack can be
// started/stopped/restarted but never routed, browser-opened, or deleted.
func stackMenuItems(running, registered, system bool) []menuItem {
	var items []menuItem
	if running {
		items = append(items,
			menuItem{"restart", "restart the stack"},
			menuItem{"down", "stop the stack's containers"},
		)
		if !system {
			items = append(items,
				menuItem{"open", "open URL in the browser"},
				menuItem{"route", "route published ports as *.localhost URLs"},
			)
		}
	} else {
		items = append(items,
			menuItem{"up", "start the stack (up, detached)"},
			menuItem{"restart", "restart the stack"},
		)
	}
	items = append(items,
		menuItem{"logs", "stream compose logs"},
		menuItem{"urls", "list reachable endpoints"},
		menuItem{"config", "show manifest / config"},
	)
	if registered {
		items = append(items, menuItem{"delete", "deregister the stack"})
	}
	return items
}

// openStackMenu populates a menu modal for the given stack row.
func (m *Model) openStackMenu(r *sidebarRow) {
	items := stackMenuItems(r.running > 0, r.selKind == selStack, r.selKind == selSystem)
	opts := make([]string, len(items))
	vals := make([]string, len(items))
	for i, it := range items {
		opts[i] = fmt.Sprintf("%-8s %s", it.token, it.desc)
		vals[i] = it.token
	}
	m.modal = modalState{
		active: true, mkind: modalMenu, stack: r.label,
		prompt: "› " + r.label + " — actions", options: opts, values: vals,
	}
}

// menuChoose dispatches the stack-menu entry at i.
func (m Model) menuChoose(i int) (tea.Model, tea.Cmd) {
	if i < 0 || i >= len(m.modal.values) {
		return m, nil
	}
	tok := m.modal.values[i]
	stack := m.modal.stack
	m.modal = modalState{}
	switch tok {
	case "up", "down", "restart":
		return m, actionStartCmd(m.eng, tok, stack)
	case "open":
		return m, openUrlCmd(m.eng, stack)
	case "delete":
		m.modal = modalState{active: true, mkind: modalConfirm, action: actDelete, stack: stack,
			prompt: fmt.Sprintf("delete stack %q?  (deregisters; containers untouched)", stack)}
		return m, nil
	case "logs":
		return m.gotoTab(tabLogs)
	case "urls":
		return m.gotoTab(tabURLs)
	case "config":
		return m.gotoTab(tabConfig)
	case "route":
		return m, routeFromCmd(m.eng, stack)
	}
	return m, nil
}

// gotoTab enters the detail view on a given tab (focus + navCmd so the tab's
// data/stream loads).
func (m Model) gotoTab(t detailTab) (tea.Model, tea.Cmd) {
	m.focus = focusDetail
	m.tab = t
	return m, m.navCmd()
}

// handleModalKey resolves the open modal. List modals (picker/menu) share
// navigation and route their selection to a chooser; confirm modals take
// y/Enter (dispatch) or n/Esc (cancel). Other keys are swallowed so the
// decision isn't bypassed.
func (m Model) handleModalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.modal.mkind {
	case modalPicker:
		return m.handleListKey(msg, m.pickerChoose)
	case modalMenu:
		return m.handleListKey(msg, m.menuChoose)
	case modalEditOpen:
		return m.handleListKey(msg, m.editOpenChoose)
	case modalOpenChoose:
		return m.handleOpenChoose(msg)
	case modalLogService:
		return m.handleListKey(msg, m.logServiceChoose)
	case modalEnvService:
		return m.handleListKey(msg, m.envServiceChoose)
	case modalSourceChoose:
		return m.handleSourceChoose(msg)
	case modalRemoveChoose:
		return m.handleRemoveChoose(msg)
	}
	switch msg.String() {
	case "y", "Y", "enter":
		act := m.modal
		m.modal = modalState{}
		return m, m.dispatchAction(act)
	case "n", "N", "esc":
		m.modal = modalState{}
		return m, nil
	}
	return m, nil
}

// handleOpenChoose resolves the transient open menu: b opens the stack's URL in
// the browser (running stacks), e opens its config/project in $EDITOR detached
// (registered stacks). esc/q cancel; unbound keys are swallowed so the menu
// stays put.
func (m Model) handleOpenChoose(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	stack := m.modal.stack
	var selK selKind
	running := false
	if r := m.rowFor(stack); r != nil {
		selK, running = r.selKind, r.running > 0
	}
	switch msg.String() {
	case "b":
		if !running {
			return m, nil // no live container ⇒ no URL to open
		}
		m.modal = modalState{}
		return m, openUrlCmd(m.eng, stack)
	case "e":
		if selK != selStack {
			return m, nil // only registered stacks have a config/project to edit
		}
		m.modal = modalState{}
		return m, editTargetsCmd(m.eng, stack)
	case "esc", "q":
		m.modal = modalState{}
		return m, nil
	}
	return m, nil
}

// handleSourceChoose resolves the transient new-stack source picker: c/t/i open
// that source's form directly; esc/q cancel. Unbound keys are swallowed so the
// transient stays put.
func (m Model) handleSourceChoose(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "c":
		m.modal = modalState{}
		return m, m.openSourceForm("compose")
	case "t":
		m.modal = modalState{}
		return m, m.openSourceForm("template")
	case "i":
		m.modal = modalState{}
		return m, m.openSourceForm("image")
	case "esc", "q":
		m.modal = modalState{}
		return m, nil
	}
	return m, nil
}

// handleRemoveChoose resolves the transient remove picker: d tears down the
// stack's containers (compose down), r deregisters a registered stack (keeps
// containers). The transient itself is the deliberate guard — no extra confirm,
// so dd is a fast teardown. compose down keeps named volumes, so data survives.
func (m Model) handleRemoveChoose(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	stack := m.modal.stack
	var selK selKind
	if r := m.rowFor(stack); r != nil {
		selK = r.selKind
	}
	switch msg.String() {
	case "d":
		m.modal = modalState{}
		// Unmanaged loose container → docker rm -f; a stack → compose down.
		if selK == selUnmanaged {
			return m, removeContainerCmd(m.eng, stack)
		}
		return m, actionStartCmd(m.eng, "down", stack)
	case "r":
		if selK != selStack {
			return m, nil // only registered stacks have a manifest to deregister
		}
		m.modal = modalState{}
		return m, removeCmd(m.eng, stack)
	case "esc", "q":
		m.modal = modalState{}
		return m, nil
	}
	return m, nil
}

// handleListKey drives a list modal: j/k move, Enter/number selects via choose,
// Esc cancels.
func (m Model) handleListKey(msg tea.KeyMsg, choose func(int) (tea.Model, tea.Cmd)) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "j", "down":
		if m.modal.cursor < len(m.modal.values)-1 {
			m.modal.cursor++
		}
		return m, nil
	case "k", "up":
		if m.modal.cursor > 0 {
			m.modal.cursor--
		}
		return m, nil
	case "enter":
		return choose(m.modal.cursor)
	case "esc", "q":
		m.modal = modalState{}
		return m, nil
	}
	// Number quick-select (1-9).
	if s := msg.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		return choose(int(s[0] - '1'))
	}
	return m, nil
}

// pickerChoose opens the URL value at i (if valid) and closes the modal.
func (m Model) pickerChoose(i int) (tea.Model, tea.Cmd) {
	if i < 0 || i >= len(m.modal.values) {
		return m, nil
	}
	url := m.modal.values[i]
	m.modal = modalState{}
	return m, openCmd(url)
}

// dispatchAction runs the confirmed action as an async engine command.
func (m Model) dispatchAction(a modalState) tea.Cmd {
	switch a.action {
	case actDelete:
		return removeCmd(m.eng, a.stack)
	case actKeep:
		return keepCmd(m.eng, a.stack)
	case actGc:
		return gcCmd(m.eng, a.stack)
	}
	return nil
}

// setToast raises a transient banner and schedules its clear. A monotonically
// increasing seq ensures a later toast's timer doesn't wipe an even newer one.
func (m *Model) setToast(s string) tea.Cmd {
	m.toast = s
	m.toastSeq++
	seq := m.toastSeq
	return tea.Tick(toastTTL, func(time.Time) tea.Msg { return toastClearMsg{seq: seq} })
}
