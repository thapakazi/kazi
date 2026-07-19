package tui

import (
	"encoding/base64"
	"testing"
	"time"
)

func TestOSC52Sequence(t *testing.T) {
	got := osc52("hi there")
	want := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte("hi there")) + "\a"
	if got != want {
		t.Fatalf("osc52 = %q, want %q", got, want)
	}
}

// stubClipboard swaps writeClipboard for a capture, restoring on cleanup.
func stubClipboard(t *testing.T) *string {
	t.Helper()
	var captured string
	orig := writeClipboard
	writeClipboard = func(s string) { captured = s }
	t.Cleanup(func() { writeClipboard = orig })
	return &captured
}

func TestCopyLinesCmdVisibleSlice(t *testing.T) {
	captured := stubClipboard(t)
	m := New(fakeEngine{}, time.Second)
	cmd := m.copyLinesCmd([]string{"a", "b", "c"})
	if *captured != "a\nb\nc" {
		t.Fatalf("clipboard = %q, want joined lines", *captured)
	}
	if cmd == nil {
		t.Fatal("copyLinesCmd should return a toast-clear command")
	}
	if m.toast != "copied 3 lines" {
		t.Fatalf("toast = %q, want %q", m.toast, "copied 3 lines")
	}
}

func TestCopyLinesCmdSingular(t *testing.T) {
	stubClipboard(t)
	m := New(fakeEngine{}, time.Second)
	m.copyLinesCmd([]string{"only"})
	if m.toast != "copied 1 line" {
		t.Fatalf("toast = %q, want singular", m.toast)
	}
}
