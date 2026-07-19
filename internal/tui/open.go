package tui

import (
	"os/exec"
	"runtime"
)

// browserOpen launches the OS default handler for a URL. It's a package var so
// tests can substitute a recorder instead of spawning a real browser.
var browserOpen = openURL

// openURL opens u with the platform's default handler (macOS `open`, Windows
// url handler, else `xdg-open`). It returns once the child is spawned.
func openURL(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}
