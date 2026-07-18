// Package labels holds kazi's label vocabulary and helpers for reading
// runtime label output and injecting kazi labels via compose overrides.
package labels

import (
	"fmt"
	"strings"
)

const (
	Managed   = "kazi.managed"   // "true" on kazi-launched containers
	Stack     = "kazi.stack"     // stack name
	Ephemeral = "kazi.ephemeral" // reserved for M2; never set in M0

	ComposeProject    = "com.docker.compose.project"
	ComposeWorkingDir = "com.docker.compose.project.working_dir"
	ComposeService    = "com.docker.compose.service"
)

// ParseDockerCSV parses docker ps's "k1=v1,k2=v2" Labels field.
// Best-effort: values containing commas are not round-trippable in this
// format; the labels kazi reads never contain commas.
func ParseDockerCSV(s string) map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		if kv == "" {
			continue
		}
		k, v, _ := strings.Cut(kv, "=")
		m[k] = v
	}
	return m
}

// OverrideYAML renders a compose override file that stamps kazi labels on
// every service. Passed as an extra -f so label injection stays pure
// compose spec (portable across docker/podman/nerdctl).
func OverrideYAML(stack string, services []string) []byte {
	var b strings.Builder
	b.WriteString("services:\n")
	for _, s := range services {
		fmt.Fprintf(&b, "  %q:\n    labels:\n      %s: \"true\"\n      %s: %q\n", s, Managed, Stack, stack)
	}
	return []byte(b.String())
}
