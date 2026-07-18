package main

import (
	"strings"
	"testing"

	"github.com/thapakazi/kazi/internal/engine"
)

// TestCommandRegistration verifies that urls, expose, and trust are registered
// as subcommands of rootCmd.
func TestCommandRegistration(t *testing.T) {
	want := map[string]bool{"urls": false, "expose": false, "trust": false}
	for _, c := range rootCmd.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("command %q not registered on rootCmd", name)
		}
	}
}

// TestExposeFlags verifies that --port defaults to 0 and --remove defaults to false.
func TestExposeFlags(t *testing.T) {
	portFlag := exposeCmd.Flags().Lookup("port")
	if portFlag == nil {
		t.Fatal("--port flag not defined on expose command")
	}
	if portFlag.DefValue != "0" {
		t.Errorf("--port default = %q, want %q", portFlag.DefValue, "0")
	}

	removeFlag := exposeCmd.Flags().Lookup("remove")
	if removeFlag == nil {
		t.Fatal("--remove flag not defined on expose command")
	}
	if removeFlag.DefValue != "false" {
		t.Errorf("--remove default = %q, want %q", removeFlag.DefValue, "false")
	}
}

// TestEndpointRowDash verifies that empty fields are rendered as "-".
func TestEndpointRowDash(t *testing.T) {
	ep := engine.Endpoint{
		Stack:   "blog",
		Service: "web",
		URL:     "https://blog.localhost",
		Target:  "web:80",
		Note:    "",
	}
	row := endpointRow(ep)
	if !strings.HasSuffix(row, "\t-") {
		t.Errorf("expected empty Note to render as '-', got: %q", row)
	}
	if !strings.HasPrefix(row, "blog\tweb\thttps://blog.localhost\tweb:80\t-") {
		t.Errorf("unexpected row format: %q", row)
	}
}

// TestEndpointRowAllDash verifies all-empty Endpoint renders all fields as "-".
func TestEndpointRowAllDash(t *testing.T) {
	row := endpointRow(engine.Endpoint{})
	parts := strings.Split(row, "\t")
	if len(parts) != 5 {
		t.Fatalf("expected 5 tab-separated fields, got %d: %q", len(parts), row)
	}
	for i, p := range parts {
		if p != "-" {
			t.Errorf("field[%d] = %q, want %q", i, p, "-")
		}
	}
}

// TestEndpointRowNoEmptyFields verifies a fully populated Endpoint has no "-" placeholders.
func TestEndpointRowNoEmptyFields(t *testing.T) {
	ep := engine.Endpoint{
		Stack:   "mystack",
		Service: "db",
		URL:     "localhost:5432",
		Target:  "db:5432",
		Note:    "allocated",
	}
	row := endpointRow(ep)
	if strings.Contains(row, "-") {
		t.Errorf("fully populated endpoint should have no '-' placeholders, got: %q", row)
	}
}
