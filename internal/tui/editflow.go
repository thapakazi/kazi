package tui

import (
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/thapakazi/kazi/internal/engine"
)

// beginEdit snapshots the target file's original bytes (for abort-restore) and
// suspends the TUI to $EDITOR on it. Validation runs when the editor returns.
func (m Model) beginEdit(stack string, target engine.EditTarget) (tea.Model, tea.Cmd) {
	orig, err := os.ReadFile(target.Path)
	if err != nil {
		return m, m.setToast("edit: " + err.Error())
	}
	m.editStack = stack
	m.editTarget = target
	m.editOrig = orig
	return m, editorExec(target.Path)
}

// editChoose begins editing the target picked from the manifest/compose picker.
func (m Model) editChoose(i int) (tea.Model, tea.Cmd) {
	if i < 0 || i >= len(m.editTargets) {
		return m, nil
	}
	target := m.editTargets[i]
	stack := m.editStack
	m.modal = modalState{}
	return m.beginEdit(stack, target)
}

// restoreEdit writes the edited file's original bytes back (abort path).
func (m *Model) restoreEdit() {
	if m.editTarget.Path != "" && m.editOrig != nil {
		_ = os.WriteFile(m.editTarget.Path, m.editOrig, 0o644)
	}
}

// clearEdit resets all edit-flow state.
func (m *Model) clearEdit() {
	m.editStack = ""
	m.editTargets = nil
	m.editTarget = engine.EditTarget{}
	m.editOrig = nil
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
