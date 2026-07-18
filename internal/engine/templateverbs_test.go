package engine

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
	"github.com/thapakazi/kazi/internal/template"
)

// TestEjectGolden verifies:
//   - compose.yml is copied byte-identical to the template's compose.yml
//   - .env contains POSTGRES_DB=app (from postgres default values)
//   - addCmd is the exact expected "kazi add <template> <dest>" string
//   - dest-exists → error
func TestEjectGolden(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("KAZI_CONFIG_DIR", tmp)

	// Change cwd to tmp so relative path "./<template>/" resolves under tmp.
	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origWd) })

	e := testEngine(t, &runtime.Fake{})

	// Eject the postgres template into the default location.
	dest, addCmd, err := e.Eject("postgres", "", false)
	if err != nil {
		t.Fatalf("Eject failed: %v", err)
	}

	// dest must exist.
	if _, statErr := os.Stat(dest); statErr != nil {
		t.Fatalf("dest %s does not exist: %v", dest, statErr)
	}

	// Verify compose.yml is byte-identical to the template's compose.yml.
	// We need the materialized template path to read the original.
	tmplDir := filepath.Join(tmp, "templates", "postgres")
	templateComposePath := filepath.Join(tmplDir, "compose.yml")
	templateBytes, readErr := os.ReadFile(templateComposePath)
	if readErr != nil {
		t.Fatalf("reading template compose.yml: %v", readErr)
	}
	ejectedComposePath := filepath.Join(dest, "compose.yml")
	ejectedBytes, readErr := os.ReadFile(ejectedComposePath)
	if readErr != nil {
		t.Fatalf("reading ejected compose.yml: %v", readErr)
	}
	if !bytes.Equal(templateBytes, ejectedBytes) {
		t.Errorf("ejected compose.yml is not byte-identical to template's:\ntemplate:\n%s\nejected:\n%s",
			templateBytes, ejectedBytes)
	}

	// Verify .env contains POSTGRES_DB=app.
	envPath := filepath.Join(dest, ".env")
	envBytes, readErr := os.ReadFile(envPath)
	if readErr != nil {
		t.Fatalf("reading .env: %v", readErr)
	}
	envContent := string(envBytes)
	if !strings.Contains(envContent, "POSTGRES_DB=app") {
		t.Errorf(".env does not contain POSTGRES_DB=app:\n%s", envContent)
	}

	// Verify addCmd is exact.
	expectedCmd := "kazi add postgres " + dest
	if addCmd != expectedCmd {
		t.Errorf("addCmd = %q, want %q", addCmd, expectedCmd)
	}

	// dest-exists → error.
	_, _, err2 := e.Eject("postgres", dest, false)
	if err2 == nil {
		t.Error("expected error when dest already exists, got nil")
	}
	if !strings.Contains(err2.Error(), "already exists") {
		t.Errorf("error should mention 'already exists': %v", err2)
	}
}

// TestEjectAddRegisters verifies that add=true causes e.Add to be called,
// producing a registered manifest with a compose source pointing into dest.
func TestEjectAddRegisters(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("KAZI_CONFIG_DIR", tmp)

	origWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(tmp); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(origWd) })

	e := testEngine(t, &runtime.Fake{})

	dest, _, err := e.Eject("postgres", "", true)
	if err != nil {
		t.Fatalf("Eject with add=true failed: %v", err)
	}

	// A manifest for "postgres" must exist with compose source pointing into dest.
	m, loadErr := store.LoadStack("postgres")
	if loadErr != nil {
		t.Fatalf("manifest not found after add=true: %v", loadErr)
	}
	if m.Spec.Source.Compose == "" {
		t.Errorf("manifest has no compose source: %+v", m.Spec.Source)
	}
	if !strings.HasPrefix(m.Spec.Source.Compose, dest) {
		t.Errorf("manifest compose source %q does not start with dest %q", m.Spec.Source.Compose, dest)
	}
	// Ejected stack must NOT reference the template — it's a plain compose stack.
	if m.Spec.Source.Template != "" {
		t.Errorf("ejected stack should have no template source, got %q", m.Spec.Source.Template)
	}
}

// TestTemplatListWrapper exercises the TemplateList delegation.
func TestTemplateListWrapper(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := testEngine(t, &runtime.Fake{})

	infos, err := e.TemplateList()
	if err != nil {
		t.Fatalf("TemplateList: %v", err)
	}
	// Embedded starters: postgres, redis, mysql, mongo, mailpit, minio (6 total).
	if len(infos) < 6 {
		t.Errorf("expected ≥6 embedded starters, got %d", len(infos))
	}
	// All should have names and be in sorted order.
	for i := 1; i < len(infos); i++ {
		if infos[i].Name < infos[i-1].Name {
			t.Errorf("infos not sorted at index %d: %q < %q", i, infos[i].Name, infos[i-1].Name)
		}
	}
}

// TestTemplateResetWrapper exercises the TemplateReset delegation.
func TestTemplateResetWrapper(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("KAZI_CONFIG_DIR", tmp)
	e := testEngine(t, &runtime.Fake{})

	// Materialize redis first so there is something to reset.
	_, err := template.Materialize("redis")
	if err != nil {
		t.Fatalf("Materialize: %v", err)
	}

	// Edit the materialized file.
	redisCompose := filepath.Join(tmp, "templates", "redis", "compose.yml")
	if err := os.WriteFile(redisCompose, []byte("# edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Reset should restore pristine.
	if err := e.TemplateReset("redis"); err != nil {
		t.Fatalf("TemplateReset: %v", err)
	}

	restored, err := os.ReadFile(redisCompose)
	if err != nil {
		t.Fatalf("reading restored compose.yml: %v", err)
	}
	if string(restored) == "# edited\n" {
		t.Error("TemplateReset did not restore the original compose.yml")
	}
	if !strings.Contains(string(restored), "redis") {
		t.Errorf("restored compose.yml does not look like the redis starter:\n%s", restored)
	}
}
