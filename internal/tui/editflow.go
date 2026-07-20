package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
)

// editOpenTarget is one thing o-e can open in the external editor: a short
// label ("config"|"project") and the path handed to $EDITOR.
type editOpenTarget struct {
	label string
	path  string
}

// editOpenTargets maps the engine's edit targets onto o-e's config/project
// choices: the manifest opens as "config" (the file itself); a compose file
// opens its containing directory as "project", so the editor loads the whole
// project rather than a single YAML. Kinds kazi doesn't own are skipped.
func editOpenTargets(targets []engine.EditTarget) []editOpenTarget {
	var out []editOpenTarget
	for _, t := range targets {
		switch t.Kind {
		case "manifest":
			out = append(out, editOpenTarget{label: "config", path: t.Path})
		case "compose":
			out = append(out, editOpenTarget{label: "project", path: filepath.Dir(t.Path)})
		}
	}
	return out
}

// editOpenChoose launches the external editor (detached) on the o-e target at i
// and closes the picker. The paths live in the modal's parallel values slice.
func (m Model) editOpenChoose(i int) (tea.Model, tea.Cmd) {
	if i < 0 || i >= len(m.modal.values) {
		return m, nil
	}
	path := m.modal.values[i]
	m.modal = modalState{}
	return m, editorOpen(path)
}

// rowFor returns the sidebar row for a named stack, or nil.
func (m Model) rowFor(name string) *sidebarRow {
	for i := range m.rows {
		if m.rows[i].kind == rowStack && m.rows[i].label == name {
			return &m.rows[i]
		}
	}
	return nil
}
