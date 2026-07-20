package tui

import (
	"testing"
)

// hintKeys extracts the ordered physical keys from a keyHint slice.
func hintKeys(hs []keyHint) []string {
	out := make([]string, len(hs))
	for i, h := range hs {
		out[i] = h.Key
	}
	return out
}

func eq(a, b []string) bool {
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

func TestContextualKeys(t *testing.T) {
	cases := []struct {
		name string
		sel  selection
		want []string
	}{
		{"running registered stack", selection{kind: selStack, running: true},
			[]string{"s", "l", "o", "d", "g", "T"}},
		{"stopped registered stack", selection{kind: selStack, running: false},
			[]string{"s", "l", "o", "d", "g", "T"}},
		{"watched ephemeral stack", selection{kind: selStack, running: true, watched: true},
			[]string{"k", "g", "s", "l", "o", "d", "g", "T"}},
		{"running discovered stack", selection{kind: selDiscovered, running: true},
			[]string{"s", "l", "o", "d", "g", "T"}},
		{"stopped discovered stack", selection{kind: selDiscovered, running: false},
			[]string{"s", "l", "d", "g", "T"}},
		{"unmanaged", selection{kind: selUnmanaged},
			[]string{"a", "d", "g", "T"}},
		{"system", selection{kind: selSystem},
			[]string{"s", "l", "T", "g", "T"}},
		{"template", selection{kind: selTemplate},
			[]string{"t", "e", "g", "T"}},
		{"all/none", selection{kind: selNone},
			[]string{"g", "T"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := hintKeys(contextualKeys(tc.sel))
			if !eq(got, tc.want) {
				t.Fatalf("keys = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestSystemNeverDelete asserts the protected-stack invariant: the delete
// action (x) is never offered on kazi-proxy.
func TestSystemNeverDelete(t *testing.T) {
	for _, h := range contextualKeys(selection{kind: selSystem}) {
		if h.Key == "x" || h.Key == "D" {
			t.Fatalf("system stack must never offer a delete action (%s)", h.Key)
		}
	}
}

// TestDiscoveredNeverDelete: discovered projects have no manifest, so delete is
// never offered on them.
func TestDiscoveredNeverDelete(t *testing.T) {
	for _, running := range []bool{true, false} {
		for _, h := range contextualKeys(selection{kind: selDiscovered, running: running}) {
			if h.Key == "x" {
				t.Fatalf("discovered stack must never offer x:delete (running=%v)", running)
			}
		}
	}
}

// TestUnmanagedActions asserts unmanaged rows offer a:adopt and d:remove among
// per-selection actions (plus the always-present globals g/T), and nothing that
// implies a manifest (no edit/deregister-style keys).
func TestUnmanagedActions(t *testing.T) {
	got := hintKeys(contextualKeys(selection{kind: selUnmanaged}))
	if !eq(got, []string{"a", "d", "g", "T"}) {
		t.Fatalf("unmanaged keys = %v, want [a d g T]", got)
	}
	forbidden := map[string]bool{"u": true, "r": true, "l": true, "x": true, "D": true, "t": true, "e": true}
	for _, k := range got {
		if forbidden[k] {
			t.Fatalf("unmanaged offered %q; only a:adopt / d:remove (+ global g/T) allowed", k)
		}
	}
}
