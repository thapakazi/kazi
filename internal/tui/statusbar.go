package tui

import (
	"fmt"
	"strings"
)

// statusBarLine builds the always-on doctor-lite header content (runtime, proxy
// health, gc-reclaimable count, staleness flag). The outer full-width style is
// applied by View.
func (m Model) statusBarLine() string {
	proxy := "●"
	if !m.proxyUp {
		proxy = "✕"
	}
	parts := []string{
		"kazi",
		"runtime:" + m.runtimeName,
		"proxy:" + proxy,
		fmt.Sprintf("gc:%d", m.gcCount),
	}
	line := strings.Join(parts, " ─ ")
	if m.stale {
		line += "  " + m.st.statusStale.Render("(stale)")
	}
	if m.toast != "" {
		line += "  " + m.st.toast.Render(m.toast)
	}
	return line
}

// renderModal draws the centered modal box: a yes/no confirm, a list picker, or
// the transient new-stack source chooser.
func (m Model) renderModal() string {
	if m.modal.mkind == modalSourceChoose {
		var b strings.Builder
		b.WriteString(m.modal.prompt + "\n\n")
		for _, sc := range sourceChoices {
			b.WriteString("  " + m.st.keybarKey.Render(sc.key) + "   " +
				fmt.Sprintf("%-9s %s", sc.name, sc.desc) + "\n")
		}
		b.WriteString("\n" + m.st.keybarKey.Render("esc") + " cancel")
		return m.st.modalBox.Render(b.String())
	}
	if m.modal.mkind == modalOpenChoose {
		var selK selKind
		running := false
		if r := m.rowFor(m.modal.stack); r != nil {
			selK, running = r.selKind, r.running > 0
		}
		var b strings.Builder
		b.WriteString(m.modal.prompt + "\n\n")
		if running {
			b.WriteString("  " + m.st.keybarKey.Render("b") + "   open URL in the browser\n")
		}
		if selK == selStack {
			b.WriteString("  " + m.st.keybarKey.Render("e") + "   open config / project in $EDITOR (detached)\n")
		}
		b.WriteString("\n" + m.st.keybarKey.Render("esc") + " cancel")
		return m.st.modalBox.Render(b.String())
	}
	if m.modal.mkind == modalRemoveChoose {
		var selK selKind
		if r := m.rowFor(m.modal.stack); r != nil {
			selK = r.selKind
		}
		var b strings.Builder
		b.WriteString(m.modal.prompt + "\n\n")
		if selK == selUnmanaged {
			b.WriteString("  " + m.st.keybarKey.Render("d") + "   remove container — docker rm -f (unmanaged; irreversible)\n")
		} else {
			b.WriteString("  " + m.st.keybarKey.Render("d") + "   down & remove containers (compose down; keeps volumes)\n")
			if selK == selStack {
				b.WriteString("  " + m.st.keybarKey.Render("r") + "   deregister — remove from kazi (containers untouched)\n")
			}
		}
		b.WriteString("\n" + m.st.keybarKey.Render("esc") + " cancel")
		return m.st.modalBox.Render(b.String())
	}
	if m.modal.mkind == modalPicker || m.modal.mkind == modalMenu || m.modal.mkind == modalEditOpen || m.modal.mkind == modalLogService || m.modal.mkind == modalEnvService {
		verb := "open"
		switch m.modal.mkind {
		case modalMenu:
			verb = "run"
		case modalLogService, modalEnvService:
			verb = "filter"
		}
		var b strings.Builder
		b.WriteString(m.modal.prompt + "\n\n")
		for i, opt := range m.modal.options {
			line := fmt.Sprintf("%d  %s", i+1, opt)
			if i == m.modal.cursor {
				b.WriteString(m.st.selectedRow.Render("▸ " + line))
			} else {
				b.WriteString("  " + line)
			}
			b.WriteByte('\n')
		}
		b.WriteString("\n" + m.st.keybarKey.Render("↵") + " " + verb + "    " +
			m.st.keybarKey.Render("esc") + " cancel")
		return m.st.modalBox.Render(b.String())
	}
	body := m.modal.prompt + "\n\n" + m.st.keybarKey.Render("y") + " confirm    " +
		m.st.keybarKey.Render("n") + " cancel"
	return m.st.modalBox.Render(body)
}

// renderForm draws the centered input form (create n / try t): a title, one
// line per field (the focused one marked, choice fields as ‹ a · B · c ›,
// must-change fields flagged with *), an inline error, and the key legend.
func (m Model) renderForm() string {
	var b strings.Builder
	b.WriteString(m.form.title + "\n\n")
	hasChoice := false
	for i, f := range m.form.fields {
		label := f.label
		if f.mustChange {
			label += " *"
		}
		var display string
		if f.kind == fieldChoice {
			hasChoice = true
			display = renderChoice(f)
		} else {
			caret := ""
			if i == m.form.cursor {
				caret = "▏"
			}
			display = f.value + caret
		}
		row := fmt.Sprintf("%-30s %s", label+":", display)
		if i == m.form.cursor {
			b.WriteString(m.st.selectedRow.Render("▸ " + row))
		} else {
			b.WriteString("  " + row)
		}
		b.WriteByte('\n')
	}
	if m.form.err != "" {
		b.WriteString("\n" + m.st.statusStale.Render(m.form.err) + "\n")
	}
	legend := "\n" + m.st.keybarKey.Render("↵") + " submit    " +
		m.st.keybarKey.Render("tab") + " next    "
	if hasChoice {
		legend += m.st.keybarKey.Render("←/→") + " change    "
	}
	legend += m.st.keybarKey.Render("esc") + " cancel"
	b.WriteString(legend)
	return m.st.modalBox.Render(b.String())
}

// renderChoice renders a choice field as ‹ a · B · c › with the selected option
// upper-cased for emphasis.
func renderChoice(f formField) string {
	parts := make([]string, len(f.choices))
	for j, c := range f.choices {
		if j == f.choiceIdx {
			parts[j] = strings.ToUpper(c)
		} else {
			parts[j] = c
		}
	}
	return "‹ " + strings.Join(parts, " · ") + " ›"
}

// keybarSegs builds the contextual keybar and, alongside the rendered string,
// the clickable column spans (mouse.go hit-tests these). The action bindings
// valid for the current selection come first, then the mode toggle and help —
// each a distinct zone so a click resolves to exactly one target.
func (m Model) keybarSegs() (string, []segment) {
	var b segBuilder
	first := true
	sep := func() {
		if !first {
			b.push("", "  ")
		}
		first = false
	}
	// On the Logs tab the keybar shows the log controls; elsewhere the
	// selection's contextual actions.
	hints := contextualKeys(m.currentSelection())
	switch {
	case m.onLogsTab():
		hints = logKeyHints()
	case m.onEnvTab():
		hints = envKeyHints()
	}
	for _, h := range hints {
		sep()
		b.push("act:"+h.Key, m.st.keybarKey.Render(h.Key)+":"+h.Label)
	}
	// n:new — register a stack; a Stacks-mode front door over `kazi add`.
	if m.mode == modeStacks && !m.onLogsTab() && !m.onEnvTab() {
		sep()
		b.push("act:n", m.st.keybarKey.Render("n")+":new")
	}
	// Action-panel toggle, only while there's captured action output.
	if m.actionTitle != "" {
		sep()
		b.push("action", m.st.keybarKey.Render("`")+":actions")
	}
	sep()
	modeLabel := "Tab:catalog"
	if m.mode == modeCatalog {
		modeLabel = "Tab:stacks"
	}
	b.push("mode", modeLabel)
	sep()
	b.push("help", m.st.keybarKey.Render("?")+":help")
	return b.String(), b.segs
}

// keybarLine is the rendered keybar without the click spans.
func (m Model) keybarLine() string {
	s, _ := m.keybarSegs()
	return s
}

// renderHelp draws the full keymap overlay for the current context.
func (m Model) renderHelp() string {
	lines := []string{
		"kazi — keymap",
		"",
		"  q / Ctrl-c   quit",
		"  ?            toggle this help",
		"  Tab / 1 2    switch mode (Stacks ↔ Catalog)",
		"  /            filter list       Esc  clear / close",
		"  R            force refresh",
		"",
		"  j / k        down / up (↓/↑ alias)",
		"  g / G        top / bottom",
		"  Ctrl-d / -u  half page down / up",
		"  h / l        focus sidebar ↔ detail",
		"  [ / ]        prev / next tab",
		"  Enter        descend into detail",
		"",
		"  c            filter by container (Logs & Env tabs · all ⇄ one service)",
		"  z            toggle fullscreen logs (Logs tab · Esc exits)",
		"  Env tab      per-container env (c filter · / search · n next · j/k scroll · y copy)",
		"  s            stack actions menu (up/down/restart/logs/open/delete)",
		"  o            open menu — b: URL in browser · e: config/project in $EDITOR",
		"  d            remove (stack: down & remove · r deregister · loose: rm -f)",
		"  a            adopt a loose container into a kazi stack",
		"  `            toggle the actions panel",
		"  n            new stack (compose/template/image · ←/→ picks source)",
		"  t            try a template (Catalog) k  keep · g  gc (watched try)",
		"  mouse        click rows/tabs/keybar · wheel scrolls",
		"  actions are contextual — see the keybar",
		"",
		"  icons  ◆ registered   ◇ discovered   □ unmanaged   ⬢ system",
		"         colour = health (green up · grey stopped · amber partial)",
	}
	return m.st.helpBox.Render(strings.Join(lines, "\n"))
}
