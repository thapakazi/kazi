package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// overlay composites box centered over base, returning a frame where only the
// cells the box covers are replaced — the rest of base shows through. This makes
// modals float over the dashboard instead of blanking it. base is assumed to be
// width×height, ANSI-styled; ansi.Cut keeps escape sequences intact when
// slicing each row.
func overlay(base, box string, width, height int) string {
	lines := strings.Split(base, "\n")
	boxLines := strings.Split(box, "\n")

	boxW := 0
	for _, bl := range boxLines {
		if w := lipgloss.Width(bl); w > boxW {
			boxW = w
		}
	}
	top := (height - len(boxLines)) / 2
	if top < 0 {
		top = 0
	}
	left := (width - boxW) / 2
	if left < 0 {
		left = 0
	}

	for i, bl := range boxLines {
		row := top + i
		if row < 0 || row >= len(lines) {
			continue
		}
		leftPart := ansi.Cut(lines[row], 0, left)
		if gap := left - ansi.StringWidth(leftPart); gap > 0 {
			leftPart += strings.Repeat(" ", gap)
		}
		rightPart := ansi.Cut(lines[row], left+lipgloss.Width(bl), width)
		lines[row] = leftPart + "\x1b[0m" + bl + "\x1b[0m" + rightPart
	}
	return strings.Join(lines, "\n")
}
