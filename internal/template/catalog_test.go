package template_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thapakazi/kazi/internal/template"
)

func TestListIncludesEmbedded(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	infos, err := template.List()
	if err != nil {
		t.Fatal(err)
	}

	wantNames := []string{"mailpit", "minio", "mongo", "mysql", "postgres", "redis"}
	found := map[string]string{} // name → description
	for _, info := range infos {
		found[info.Name] = info.Description
	}

	for _, name := range wantNames {
		desc, ok := found[name]
		if !ok {
			t.Errorf("embedded starter %q missing from List()", name)
			continue
		}
		if desc == "" {
			t.Errorf("starter %q has empty description", name)
		}
	}
}

func TestMaterializeOnceNeverOverwrites(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	dir, err := template.Materialize("postgres")
	if err != nil {
		t.Fatal(err)
	}
	if dir == "" {
		t.Fatal("Materialize returned empty dir")
	}

	// Edit compose.yml
	composeFile := filepath.Join(dir, "compose.yml")
	original, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatal(err)
	}
	edited := string(original) + "# user edit\n"
	if err := os.WriteFile(composeFile, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	// Materialize again — must not overwrite
	dir2, err := template.Materialize("postgres")
	if err != nil {
		t.Fatal(err)
	}
	if dir2 != dir {
		t.Errorf("Materialize returned different dir: got %q, want %q", dir2, dir)
	}

	after, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != edited {
		t.Error("Materialize overwrote user-edited compose.yml")
	}
}

func TestReset(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	// Materialize and edit
	dir, err := template.Materialize("postgres")
	if err != nil {
		t.Fatal(err)
	}
	composeFile := filepath.Join(dir, "compose.yml")
	original, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatal(err)
	}
	edited := string(original) + "# user edit\n"
	if err := os.WriteFile(composeFile, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reset → pristine
	if err := template.Reset("postgres"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(composeFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(after), "# user edit") {
		t.Error("Reset did not restore pristine compose.yml")
	}
	if string(after) != string(original) {
		t.Errorf("Reset content mismatch:\ngot:  %s\nwant: %s", after, original)
	}

	// Reset unknown name → error
	if err := template.Reset("nonexistent-xyz"); err == nil {
		t.Error("Reset of unknown name should error")
	}

	// Reset non-embedded (imported) template → error
	importDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(importDir, "compose.yml"), []byte("services:\n  x:\n    image: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = template.Import(importDir, "custom-local")
	if err != nil {
		t.Fatal(err)
	}
	if err := template.Reset("custom-local"); err == nil {
		t.Error("Reset of non-embedded template should error")
	}
}

func TestImportDir(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	// Valid dir with compose.yml
	srcDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(srcDir, "compose.yml"), []byte("services:\n  web:\n    image: nginx\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	info, err := template.Import(srcDir, "myapp")
	if err != nil {
		t.Fatalf("Import failed: %v", err)
	}
	if info.Name != "myapp" {
		t.Errorf("info.Name = %q, want myapp", info.Name)
	}
	if info.Embedded {
		t.Error("imported template should not be Embedded")
	}

	// Compose file should be present in catalog
	destCompose := filepath.Join(info.Path, "compose.yml")
	if _, err := os.Stat(destCompose); err != nil {
		t.Errorf("compose.yml not found at %s: %v", destCompose, err)
	}

	// No compose file → error
	emptyDir := t.TempDir()
	_, err = template.Import(emptyDir, "empty")
	if err == nil {
		t.Error("Import of dir without compose file should error")
	}

	// Collision → error containing "already exists"
	_, err = template.Import(srcDir, "myapp")
	if err == nil {
		t.Error("Import collision should error")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("collision error should contain 'already exists', got: %v", err)
	}
}
