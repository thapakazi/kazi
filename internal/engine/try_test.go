package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thapakazi/kazi/internal/proxy"
	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

// minimalTemplateConfigJSON is a compose config JSON suitable for template
// stacks (single service, no routing complexity).
const minimalTemplateConfigJSON = `{"services":{"postgres":{"networks":{"default":null}}}}`

// TestTryCreatesEphemeralManifest: manifest exists, Ephemeral:true, CreatedAt
// parses RFC3339, source.template set, compose up recorded with env from --set.
func TestTryCreatesEphemeralManifest(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{ConfigJSON: minimalTemplateConfigJSON}
	e := testEngine(t, f)

	name, _, err := e.Try(t.Context(), "postgres", TryOpts{
		Sets: []string{"postgres_db=mydb"},
	})
	if err != nil {
		t.Fatal(err)
	}

	m, err := store.LoadStack(name)
	if err != nil {
		t.Fatalf("manifest missing after Try: %v", err)
	}

	// Manifest must be ephemeral.
	if !m.Spec.Ephemeral {
		t.Error("Ephemeral must be true by default")
	}

	// Source must be the template name.
	if m.Spec.Source.Template != "postgres" {
		t.Errorf("source.template = %q, want postgres", m.Spec.Source.Template)
	}

	// CreatedAt must parse as RFC3339.
	if m.Metadata.CreatedAt == "" {
		t.Fatal("CreatedAt not set")
	}
	ts, parseErr := time.Parse(time.RFC3339, m.Metadata.CreatedAt)
	if parseErr != nil {
		t.Errorf("CreatedAt %q does not parse as RFC3339: %v", m.Metadata.CreatedAt, parseErr)
	}
	if time.Since(ts) > 10*time.Second {
		t.Errorf("CreatedAt %q looks stale (>10s ago)", m.Metadata.CreatedAt)
	}

	// Values must record the --set override.
	if m.Spec.Values["postgres_db"] != "mydb" {
		t.Errorf("values[postgres_db] = %q, want mydb; all values: %v", m.Spec.Values["postgres_db"], m.Spec.Values)
	}

	// Compose up must have been called with POSTGRES_DB=mydb in env.
	found := false
	for _, env := range f.Envs() {
		for _, kv := range env {
			if kv == "POSTGRES_DB=mydb" {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("compose up must carry POSTGRES_DB=mydb in env; envs=%v", f.Envs())
	}
}

// TestTryKeepFlag: Keep:true ⇒ Ephemeral:false.
func TestTryKeepFlag(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{ConfigJSON: minimalTemplateConfigJSON}
	e := testEngine(t, f)

	name, _, err := e.Try(t.Context(), "postgres", TryOpts{Keep: true})
	if err != nil {
		t.Fatal(err)
	}

	m, err := store.LoadStack(name)
	if err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
	if m.Spec.Ephemeral {
		t.Error("Ephemeral must be false when Keep:true")
	}
}

// TestTryNameCollision: existing stack of same name ⇒ -2 suffix used.
func TestTryNameCollision(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	// Pre-register a stack named "postgres" so there is a collision.
	existing := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	existing.Metadata.Name = "postgres"
	existing.Spec.Source.Template = "postgres"
	if err := store.SaveStack(existing); err != nil {
		t.Fatal(err)
	}

	f := &runtime.Fake{ConfigJSON: minimalTemplateConfigJSON}
	e := testEngine(t, f)

	name, _, err := e.Try(t.Context(), "postgres", TryOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if name != "postgres-2" {
		t.Errorf("name = %q, want postgres-2", name)
	}

	// The new manifest must exist under postgres-2.
	if _, err := store.LoadStack("postgres-2"); err != nil {
		t.Errorf("postgres-2 manifest missing: %v", err)
	}
}

// TestTeardownZeroResidue: after Teardown:
// - manifest gone (ErrNotFound)
// - FreeStack emptied allocations
// - down call contains -v AND --rmi local
// - Caddyfile route gone
func TestTeardownZeroResidue(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{ConfigJSON: minimalTemplateConfigJSON}
	e := testEngine(t, f)

	// First Try so a manifest and up exist.
	name, _, err := e.Try(t.Context(), "postgres", TryOpts{})
	if err != nil {
		t.Fatal(err)
	}

	// Seed a fake port allocation for this stack.
	ps, _ := proxy.LoadPorts()
	_, _ = ps.Allocate(name, "postgres", 5432, 0, 42000, 42999)

	// Reset call recorder so we only see teardown calls.
	f.Calls = nil
	f.Cmds = nil

	if err := e.Teardown(t.Context(), name); err != nil {
		t.Fatalf("Teardown error: %v", err)
	}

	// Manifest must be gone.
	if _, err := store.LoadStack(name); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("manifest still present after Teardown: %v", err)
	}

	// Port allocations must be freed.
	ps2, _ := proxy.LoadPorts()
	if allocs := ps2.Services(name); len(allocs) != 0 {
		t.Errorf("allocations not freed: %+v", allocs)
	}

	// Down call must carry -v and --rmi local.
	allCalls := joinCmds(f.Calls)
	if !strings.Contains(allCalls, "-v") {
		t.Errorf("down call must contain -v:\n%s", allCalls)
	}
	if !strings.Contains(allCalls, "--rmi") || !strings.Contains(allCalls, "local") {
		t.Errorf("down call must contain --rmi local:\n%s", allCalls)
	}

	// Caddyfile must not contain the stack route.
	caddyPath := filepath.Join(proxy.Dir(), "Caddyfile")
	if b, err := os.ReadFile(caddyPath); err == nil {
		if strings.Contains(string(b), name+".localhost") {
			t.Errorf("Caddyfile still contains route for %s: %s", name, b)
		}
	}
}

// TestTeardownDownFailureKeepsManifest: Fake fails the compose down call ⇒
// error returned AND manifest still present (gc-recoverable state).
func TestTeardownDownFailureKeepsManifest(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	// Register a template stack manually.
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = "postgres"
	m.Spec.Source.Template = "postgres"
	m.Spec.Ephemeral = true
	m.Metadata.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	if err := store.SaveStack(m); err != nil {
		t.Fatal(err)
	}

	// Fake that fails on "down".
	f := &runtime.Fake{
		ConfigJSON:      minimalTemplateConfigJSON,
		FailComposeArgs: []string{"down"},
	}
	e := testEngine(t, f)

	err := e.Teardown(t.Context(), "postgres")
	if err == nil {
		t.Error("Teardown must return error when down fails")
	}

	// Manifest must still be present so gc can retry.
	if _, loadErr := store.LoadStack("postgres"); loadErr != nil {
		t.Errorf("manifest must be present after failed Teardown, got: %v", loadErr)
	}
}

// TestKeepEditsManifestOnly: Keep flips ephemeral to false; no runtime calls.
func TestKeepEditsManifestOnly(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	// Register an ephemeral stack.
	m := store.Manifest{APIVersion: "kazi.dev/v1alpha1", Kind: "Stack"}
	m.Metadata.Name = "postgres"
	m.Spec.Source.Template = "postgres"
	m.Spec.Ephemeral = true
	if err := store.SaveStack(m); err != nil {
		t.Fatal(err)
	}

	f := &runtime.Fake{}
	e := testEngine(t, f)

	if err := e.Keep("postgres"); err != nil {
		t.Fatalf("Keep error: %v", err)
	}

	// Ephemeral must now be false.
	after, err := store.LoadStack("postgres")
	if err != nil {
		t.Fatal(err)
	}
	if after.Spec.Ephemeral {
		t.Error("Keep must set Ephemeral to false")
	}

	// No runtime calls (no compose, no rt.Cmd).
	if len(f.Calls) != 0 || len(f.Cmds) != 0 {
		t.Errorf("Keep must make zero runtime calls; Calls=%v Cmds=%v", f.Calls, f.Cmds)
	}
}
