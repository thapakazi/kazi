package template_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/thapakazi/kazi/internal/template"
)

func TestLoadValuesFlattening(t *testing.T) {
	dir := t.TempDir()
	yaml := `description: Test service
name: myservice
port: 8080
enabled: true
database:
  host: localhost
  port: 5432
`
	if err := os.WriteFile(filepath.Join(dir, "values.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	desc, vals, err := template.LoadValues(dir)
	if err != nil {
		t.Fatal(err)
	}

	if desc != "Test service" {
		t.Errorf("desc = %q, want %q", desc, "Test service")
	}
	if _, ok := vals["description"]; ok {
		t.Error("description should not appear in vals map")
	}
	if vals["name"] != "myservice" {
		t.Errorf("vals[name] = %q, want myservice", vals["name"])
	}
	if vals["port"] != "8080" {
		t.Errorf("vals[port] = %q, want 8080", vals["port"])
	}
	if vals["enabled"] != "true" {
		t.Errorf("vals[enabled] = %q, want true", vals["enabled"])
	}
	if vals["database_host"] != "localhost" {
		t.Errorf("vals[database_host] = %q, want localhost", vals["database_host"])
	}
	if vals["database_port"] != "5432" {
		t.Errorf("vals[database_port] = %q, want 5432", vals["database_port"])
	}
}

func TestMergeValues(t *testing.T) {
	base := map[string]string{
		"host": "localhost",
		"port": "5432",
		"name": "app",
	}

	// Override + case-insensitive key normalization
	result, err := template.MergeValues(base, []string{"PORT=6543", "NAME=mydb"})
	if err != nil {
		t.Fatal(err)
	}
	if result["port"] != "6543" {
		t.Errorf("result[port] = %q, want 6543", result["port"])
	}
	if result["name"] != "mydb" {
		t.Errorf("result[name] = %q, want mydb", result["name"])
	}
	if result["host"] != "localhost" {
		t.Errorf("result[host] = %q, want localhost (unchanged)", result["host"])
	}

	// Bad entry without '=' → error
	_, err = template.MergeValues(base, []string{"badkey"})
	if err == nil {
		t.Error("MergeValues with 'badkey' (no '=') should error")
	}

	// Case-insensitive: KEY= same as key=
	result2, err := template.MergeValues(base, []string{"HOST=remotehost"})
	if err != nil {
		t.Fatal(err)
	}
	if result2["host"] != "remotehost" {
		t.Errorf("result2[host] = %q, want remotehost", result2["host"])
	}
}

func TestFlattenEnv(t *testing.T) {
	vals := map[string]string{
		"postgres_password": "secret",
		"postgres_db":       "app",
		"postgres_tag":      "17-alpine",
	}

	env := template.FlattenEnv(vals)

	// Must be sorted
	expected := []string{
		"POSTGRES_DB=app",
		"POSTGRES_PASSWORD=secret",
		"POSTGRES_TAG=17-alpine",
	}
	if len(env) != len(expected) {
		t.Fatalf("FlattenEnv len = %d, want %d; got %v", len(env), len(expected), env)
	}
	for i, want := range expected {
		if env[i] != want {
			t.Errorf("env[%d] = %q, want %q", i, env[i], want)
		}
	}
}
