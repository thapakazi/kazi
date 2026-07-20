package tui

import (
	"testing"
)

// stubEmacsClient makes editorPlan believe emacsclient is (un)available.
func stubEmacsClient(t *testing.T, path string, ok bool) {
	t.Helper()
	prev := emacsClientPath
	emacsClientPath = func() (string, bool) { return path, ok }
	t.Cleanup(func() { emacsClientPath = prev })
}

func eqArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestEditorPlanEmacsPrefersClient: with emacsclient available, an emacs editor
// opens through the client (detached) so it reuses a running emacs; -a falls
// back to the configured emacs when no server is up.
func TestEditorPlanEmacsPrefersClient(t *testing.T) {
	stubEmacsClient(t, "/usr/bin/emacsclient", true)
	name, args, detach := editorPlan("emacs", "/cfg/api.yaml")
	if name != "/usr/bin/emacsclient" || !detach {
		t.Fatalf("name=%q detach=%v, want emacsclient path + detached", name, detach)
	}
	if !eqArgs(args, []string{"-n", "-a", "emacs", "/cfg/api.yaml"}) {
		t.Fatalf("args = %v, want [-n -a emacs /cfg/api.yaml]", args)
	}
}

// TestEditorPlanEmacsNoClientFallsBack: without emacsclient, plain emacs GUI is
// launched directly and still detached.
func TestEditorPlanEmacsNoClientFallsBack(t *testing.T) {
	stubEmacsClient(t, "", false)
	name, args, detach := editorPlan("emacs", "/cfg/api.yaml")
	if name != "emacs" || !detach || !eqArgs(args, []string{"/cfg/api.yaml"}) {
		t.Fatalf("plan = %q %v detach=%v, want emacs [/cfg/api.yaml] detached", name, args, detach)
	}
}

// TestEditorPlanEmacsTerminalSuspends: emacs -nw is a terminal frame, so it must
// NOT be routed through the client and must run suspended (needs a TTY).
func TestEditorPlanEmacsTerminalSuspends(t *testing.T) {
	stubEmacsClient(t, "/usr/bin/emacsclient", true) // available but must be ignored
	name, args, detach := editorPlan("emacs -nw", "/x")
	if name != "emacs" || detach {
		t.Fatalf("name=%q detach=%v, want emacs + suspend", name, detach)
	}
	if !eqArgs(args, []string{"-nw", "/x"}) {
		t.Fatalf("args = %v, want [-nw /x]", args)
	}
}

// TestEditorPlanTerminalEditors: vim/nano/vi and unknown editors run suspended
// (they need a TTY); flags and full paths are preserved.
func TestEditorPlanTerminalEditors(t *testing.T) {
	cases := []struct {
		editor   string
		wantName string
		wantArgs []string
	}{
		{"vim", "vim", []string{"/x"}},
		{"nano", "nano", []string{"/x"}},
		{"/usr/bin/vi", "/usr/bin/vi", []string{"/x"}},
		{"nvim -p", "nvim", []string{"-p", "/x"}},
		{"weirdedit", "weirdedit", []string{"/x"}},
	}
	for _, tc := range cases {
		name, args, detach := editorPlan(tc.editor, "/x")
		if name != tc.wantName || detach || !eqArgs(args, tc.wantArgs) {
			t.Fatalf("editorPlan(%q) = %q %v detach=%v, want %q %v suspend",
				tc.editor, name, args, detach, tc.wantName, tc.wantArgs)
		}
	}
}

// TestEditorPlanGUIEditorsDetach: known GUI editors run detached with the path
// (and any flags) appended.
func TestEditorPlanGUIEditorsDetach(t *testing.T) {
	cases := []struct {
		editor   string
		wantName string
		wantArgs []string
	}{
		{"code -w", "code", []string{"-w", "/x"}},
		{"subl", "subl", []string{"/x"}},
		{"gvim", "gvim", []string{"/x"}},
		{"/opt/zed/zed", "/opt/zed/zed", []string{"/x"}},
	}
	for _, tc := range cases {
		name, args, detach := editorPlan(tc.editor, "/x")
		if name != tc.wantName || !detach || !eqArgs(args, tc.wantArgs) {
			t.Fatalf("editorPlan(%q) = %q %v detach=%v, want %q %v detached",
				tc.editor, name, args, detach, tc.wantName, tc.wantArgs)
		}
	}
}

// TestEditorPlanEmacsFullPathClient: a full-path emacs is recognised by its base
// name and routed through the client, preserving the configured command as the
// -a fallback.
func TestEditorPlanEmacsFullPathClient(t *testing.T) {
	stubEmacsClient(t, "/usr/local/bin/emacsclient", true)
	name, args, detach := editorPlan("/usr/local/bin/emacs", "/proj")
	if name != "/usr/local/bin/emacsclient" || !detach {
		t.Fatalf("name=%q detach=%v, want emacsclient + detached", name, detach)
	}
	if !eqArgs(args, []string{"-n", "-a", "/usr/local/bin/emacs", "/proj"}) {
		t.Fatalf("args = %v", args)
	}
}
