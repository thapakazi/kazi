package tui

import "github.com/charmbracelet/lipgloss"

// Health is shown as the colour of each row's kind icon (see kindGlyph):
// green = up, grey = stopped, amber = partial. glyphStyle resolves the colour.

type styles struct {
	statusBar      lipgloss.Style
	statusStale    lipgloss.Style
	sidebar        lipgloss.Style
	detail         lipgloss.Style
	groupHeader    lipgloss.Style
	selectedRow    lipgloss.Style
	selectedRowDim lipgloss.Style
	row            lipgloss.Style
	tabActive      lipgloss.Style
	tabInactive    lipgloss.Style
	keybar         lipgloss.Style
	keybarKey      lipgloss.Style
	helpBox        lipgloss.Style
	modalBox       lipgloss.Style
	toast          lipgloss.Style
	stackLabel     lipgloss.Style
	actionBar      lipgloss.Style
	actionLine     lipgloss.Style
	glyphUp        lipgloss.Style
	glyphStopped   lipgloss.Style
	glyphPartial   lipgloss.Style
}

func defaultStyles() styles {
	return styles{
		statusBar:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("15")),
		statusStale: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
		sidebar:     lipgloss.NewStyle().Padding(0, 1),
		detail: lipgloss.NewStyle().Padding(0, 2).
			BorderStyle(lipgloss.NormalBorder()).BorderLeft(true).
			BorderForeground(lipgloss.Color("238")),
		groupHeader: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("244")),
		// Active selection (sidebar focused): bright blue bar. Dim selection
		// (focus in the detail pane): subtle grey bar so you still see where you
		// are without it competing with the active pane.
		selectedRow:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("12")),
		selectedRowDim: lipgloss.NewStyle().Foreground(lipgloss.Color("15")).Background(lipgloss.Color("238")),
		row:            lipgloss.NewStyle(),
		tabActive:      lipgloss.NewStyle().Bold(true).Underline(true).Foreground(lipgloss.Color("12")),
		tabInactive:    lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		keybar:         lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		keybarKey:      lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("12")),
		helpBox:        lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 2),
		modalBox: lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 3).
			BorderForeground(lipgloss.Color("203")),
		toast: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).
			Background(lipgloss.Color("214")).Padding(0, 1),
		stackLabel:   lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("13")),
		actionBar:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("0")).Background(lipgloss.Color("109")),
		actionLine:   lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		glyphUp:      lipgloss.NewStyle().Foreground(lipgloss.Color("10")),
		glyphStopped: lipgloss.NewStyle().Foreground(lipgloss.Color("244")),
		glyphPartial: lipgloss.NewStyle().Foreground(lipgloss.Color("214")),
	}
}

// glyphStyle colors a glyph by its running/total tally.
func (s styles) glyphStyle(running, total int) lipgloss.Style {
	switch {
	case total == 0 || running == 0:
		return s.glyphStopped
	case running == total:
		return s.glyphUp
	default:
		return s.glyphPartial
	}
}
