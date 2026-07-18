// Package labels holds kazi's label vocabulary and helpers for reading
// runtime label output and injecting kazi labels via compose overrides.
package labels

import (
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
