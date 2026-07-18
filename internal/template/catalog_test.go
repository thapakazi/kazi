package template_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thapakazi/kazi/internal/template"
)

// TestImportShadowingEmbeddedNameIsNotResettable verifies Finding 1:
// when a user imports a dir under an embedded starter's name (e.g. "postgres"),
// List() must report Embedded=false and Reset() must refuse without destroying
// the imported content.
func TestImportShadowingEmbeddedNameIsNotResettable(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	// Prepare a fixture dir to import (starter "postgres" not yet materialized).
	srcDir := t.TempDir()
	const importedContent = "services:\n  pg:\n    image: my-custom-postgres\n"
	if err := os.WriteFile(filepath.Join(srcDir, "compose.yml"), []byte(importedContent), 0o644); err != nil {
		t.Fatal(err)
	}

	// Import under the name "postgres" — must succeed because the starter hasn't
	// been materialized yet (no on-disk collision).
	info, err := template.Import(srcDir, "postgres")
	if err != nil {
		t.Fatalf("Import under embedded starter name failed: %v", err)
	}

	// List() must show postgres with Embedded=false.
	infos, err := template.List()
	if err != nil {
		t.Fatal(err)
	}
	var found *template.Info
	for i := range infos {
		if infos[i].Name == "postgres" {
			found = &infos[i]
			break
		}
	}
	if found == nil {
		t.Fatal("List() did not return postgres")
	}
	if found.Embedded {
		t.Error("List() marked imported 'postgres' as Embedded=true; want false")
	}

	// Reset("postgres") must fail — imported content must survive.
	if err := template.Reset("postgres"); err == nil {
		t.Error("Reset of imported-over-embedded-name should error, got nil")
	}

	// The imported compose.yml must be byte-for-byte intact.
	after, err := os.ReadFile(filepath.Join(info.Path, "compose.yml"))
	if err != nil {
		t.Fatalf("reading compose.yml after Reset attempt: %v", err)
	}
	if string(after) != importedContent {
		t.Errorf("Reset destroyed imported content:\ngot:  %q\nwant: %q", string(after), importedContent)
	}
}

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
