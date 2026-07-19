package tui

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// osc52 builds the OSC 52 "set clipboard" escape sequence for text:
// ESC ] 52 ; c ; <base64> BEL. Terminals (and multiplexers that forward it,
// so it works over SSH) copy the payload to the system clipboard. Pure —
// tests assert this string rather than the real clipboard.
func osc52(text string) string {
	enc := base64.StdEncoding.EncodeToString([]byte(text))
	return fmt.Sprintf("\x1b]52;c;%s\a", enc)
}

// shellClipboardCopiers are the local fallbacks tried, in order, when a
// terminal doesn't honour OSC 52. Each reads the payload from stdin.
var shellClipboardCopiers = [][]string{
	{"pbcopy"},                           // macOS
	{"wl-copy"},                          // Wayland
	{"xclip", "-selection", "clipboard"}, // X11
	{"xsel", "--clipboard", "--input"},
}

// writeClipboard is the side-effecting sink: it emits the OSC 52 sequence to
// the terminal (the primary, SSH-safe path) and additionally best-effort pipes
// the text to a local clipboard tool if one is present. It is a package var so
// tests can stub it and assert without touching the real clipboard.
var writeClipboard = func(text string) {
	// OSC 52 goes straight to the tty; the terminal intercepts it, so it never
	// disturbs the alt-screen frame.
	fmt.Fprint(os.Stdout, osc52(text))
	shellClipboardFallback(text)
}

// shellClipboardFallback pipes text into the first available local clipboard
// tool. Errors are ignored: OSC 52 already ran, this is only a belt-and-braces
// for terminals that drop it.
func shellClipboardFallback(text string) {
	for _, argv := range shellClipboardCopiers {
		if _, err := exec.LookPath(argv[0]); err != nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Stdin = strings.NewReader(text)
		_ = cmd.Run()
		cancel()
		return
	}
}

// copyLinesCmd copies lines to the clipboard and returns a toast reporting the
// count. n is passed separately so the toast can be phrased before the join.
func (m *Model) copyLinesCmd(lines []string) tea.Cmd {
	text := strings.Join(lines, "\n")
	n := len(lines)
	writeClipboard(text)
	unit := "lines"
	if n == 1 {
		unit = "line"
	}
	return m.setToast(fmt.Sprintf("copied %d %s", n, unit))
}
