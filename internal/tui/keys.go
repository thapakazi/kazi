package tui

import "github.com/charmbracelet/bubbles/key"

// keyMap is the full global + motion keymap. Contextual action bindings are
// resolved separately by contextualKeys (they drive the keybar text and, in
// wave 2, dispatch). Arrows are aliases for j/k — the vim keys are the contract.
type keyMap struct {
	Quit    key.Binding
	Help    key.Binding
	Mode    key.Binding // Tab
	Mode1   key.Binding // 1
	Mode2   key.Binding // 2
	Filter  key.Binding
	Escape  key.Binding
	Refresh key.Binding

	Down     key.Binding
	Up       key.Binding
	Top      key.Binding
	Bottom   key.Binding
	HalfDown key.Binding
	HalfUp   key.Binding
	FocusL   key.Binding // h
	FocusR   key.Binding // l
	TabPrev  key.Binding // [
	TabNext  key.Binding // ]
	Enter    key.Binding
	Actions  key.Binding // `
}

func defaultKeyMap() keyMap {
	return keyMap{
		Quit:    key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Help:    key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Mode:    key.NewBinding(key.WithKeys("tab"), key.WithHelp("Tab", "mode")),
		Mode1:   key.NewBinding(key.WithKeys("1")),
		Mode2:   key.NewBinding(key.WithKeys("2")),
		Filter:  key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "filter")),
		Escape:  key.NewBinding(key.WithKeys("esc")),
		Refresh: key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "refresh")),

		Down:     key.NewBinding(key.WithKeys("j", "down"), key.WithHelp("j/k", "move")),
		Up:       key.NewBinding(key.WithKeys("k", "up")),
		Top:      key.NewBinding(key.WithKeys("g"), key.WithHelp("g/G", "top/bottom")),
		Bottom:   key.NewBinding(key.WithKeys("G")),
		HalfDown: key.NewBinding(key.WithKeys("ctrl+d")),
		HalfUp:   key.NewBinding(key.WithKeys("ctrl+u")),
		FocusL:   key.NewBinding(key.WithKeys("h"), key.WithHelp("h/l", "focus")),
		FocusR:   key.NewBinding(key.WithKeys("l")),
		TabPrev:  key.NewBinding(key.WithKeys("["), key.WithHelp("[/]", "tab")),
		TabNext:  key.NewBinding(key.WithKeys("]")),
		Enter:    key.NewBinding(key.WithKeys("enter"), key.WithHelp("Enter", "descend")),
		Actions:  key.NewBinding(key.WithKeys("`"), key.WithHelp("`", "actions")),
	}
}

// keyHint is one contextual action shown in the keybar. Key is the physical
// key; Label is the short verb. Wave 1 only renders these and asserts them in
// tests; pressing an action key is a no-op until wave 2 wires dispatch.
type keyHint struct {
	Key   string
	Label string
}

// selKind classifies the current sidebar selection for contextual bindings.
type selKind int

const (
	selNone       selKind = iota // ALL / group header — no per-stack actions
	selStack                     // registered stack (has a manifest ⇒ deletable)
	selDiscovered                // discovered compose project (no manifest)
	selUnmanaged                 // loose container
	selSystem                    // kazi-proxy (protected)
	selTemplate                  // catalog template (Catalog mode)
)

// selection is the resolved context contextualKeys reasons over.
type selection struct {
	kind    selKind
	running bool // stack has at least one running container
	watched bool // the ephemeral stack just launched via try (offers k:keep/g:gc)
}

// contextualKeys returns, in keybar order, the action bindings valid for the
// given selection. It is pure and table-tested. It encodes the spec's
// contextual table (§Navigation): rm is NEVER offered on the system stack, and
// only a:adopt is offered on unmanaged rows. Every context ends with the global
// g:gc and T:trust actions.
func contextualKeys(sel selection) []keyHint {
	var hints []keyHint
	switch sel.kind {
	case selStack:
		// Registered stacks: s:menu, l:logs, o:open (browser/editor menu), d:remove.
		// o is always offered — its editor branch needs no running container.
		hints = []keyHint{{"s", "menu"}, {"l", "logs"}, {"o", "open"}, {"d", "remove"}}
		// A watched ephemeral stack (just launched via try) also offers keep/gc.
		if sel.watched {
			hints = append([]keyHint{{"k", "keep"}, {"g", "gc"}}, hints...)
		}
	case selDiscovered:
		// Discovered projects have no manifest; d:remove tears down their containers.
		if sel.running {
			hints = []keyHint{{"s", "menu"}, {"l", "logs"}, {"o", "open"}, {"d", "remove"}}
		} else {
			hints = []keyHint{{"s", "menu"}, {"l", "logs"}, {"d", "remove"}}
		}
	case selUnmanaged:
		// Loose containers: a:adopt (bring under kazi) or d:remove (docker rm -f).
		hints = []keyHint{{"a", "adopt"}, {"d", "remove"}}
	case selSystem:
		// System stack (kazi-proxy): startable/stoppable/restartable via the menu
		// (so a down proxy can be brought back up), but never removable. Plus
		// logs + trust.
		hints = []keyHint{{"s", "menu"}, {"l", "logs"}, {"T", "trust"}}
	case selTemplate:
		hints = []keyHint{{"t", "try"}, {"e", "eject"}}
	case selNone:
		// ALL / headers: only the global actions apply.
	}
	// Global actions available in every context.
	hints = append(hints, keyHint{"g", "gc"}, keyHint{"T", "trust"})
	return hints
}
