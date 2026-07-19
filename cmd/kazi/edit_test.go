package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/thapakazi/kazi/internal/engine"
)

// stubLaunch replaces the interactive editor launch for the duration of a test.
func stubLaunch(t *testing.T, fn func(editor, path string) error) {
	t.Helper()
	prev := launchEditor
	launchEditor = fn
	t.Cleanup(func() { launchEditor = prev })
}

// withStdin feeds input to os.Stdin (for the re-edit prompt) for a test.
func withStdin(t *testing.T, input string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString(input)
	w.Close()
	prev := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = prev; r.Close() })
}

func okValidator(context.Context) error { return nil }

func TestEditWithRecoveryValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.yaml")
	os.WriteFile(path, []byte("orig\n"), 0o644)
	stubLaunch(t, func(_, p string) error { return os.WriteFile(p, []byte("edited\n"), 0o644) })

	target := engine.EditTarget{Path: path, Kind: "manifest", Validate: okValidator}
	if err := editWithRecovery(context.Background(), nil, "vi", target); err != nil {
		t.Fatalf("valid edit should succeed, got %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "edited\n" {
		t.Fatalf("valid edit should keep the new content, got %q", got)
	}
}

func TestEditWithRecoveryAbortRestores(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.yaml")
	orig := []byte("orig\n")
	os.WriteFile(path, orig, 0o644)
	stubLaunch(t, func(_, p string) error { return os.WriteFile(p, []byte("bad\n"), 0o644) })
	withStdin(t, "n\n") // decline re-edit

	target := engine.EditTarget{Path: path, Kind: "manifest",
		Validate: func(context.Context) error { return errors.New("invalid") }}
	err := editWithRecovery(context.Background(), nil, "vi", target)
	if !errors.Is(err, errEditAborted) {
		t.Fatalf("declining re-edit should abort, got %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(orig) {
		t.Fatalf("abort must restore the original byte-for-byte, got %q", got)
	}
}

func TestEditWithRecoveryReeditThenValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.yaml")
	os.WriteFile(path, []byte("orig\n"), 0o644)
	edits := 0
	stubLaunch(t, func(_, p string) error {
		edits++
		if edits == 1 {
			return os.WriteFile(p, []byte("bad\n"), 0o644)
		}
		return os.WriteFile(p, []byte("good\n"), 0o644)
	})
	withStdin(t, "y\n") // accept re-edit

	validations := 0
	target := engine.EditTarget{Path: path, Kind: "manifest", Validate: func(context.Context) error {
		validations++
		if validations == 1 {
			return errors.New("invalid")
		}
		return nil
	}}
	if err := editWithRecovery(context.Background(), nil, "vi", target); err != nil {
		t.Fatalf("re-edit then valid should succeed, got %v", err)
	}
	if edits != 2 {
		t.Fatalf("editor should reopen once on re-edit, opened %d times", edits)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "good\n" {
		t.Fatalf("final content should be the valid re-edit, got %q", got)
	}
}

func TestEditWithRecoveryEditorErrorRestores(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "m.yaml")
	orig := []byte("orig\n")
	os.WriteFile(path, orig, 0o644)
	// Editor crashes after scribbling; the change must be discarded.
	stubLaunch(t, func(_, p string) error {
		os.WriteFile(p, []byte("half-written"), 0o644)
		return errors.New("editor crashed")
	})
	target := engine.EditTarget{Path: path, Kind: "manifest", Validate: okValidator}
	if err := editWithRecovery(context.Background(), nil, "vi", target); err == nil {
		t.Fatal("an editor error should surface as an error")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(orig) {
		t.Fatalf("editor error should restore the original, got %q", got)
	}
}
