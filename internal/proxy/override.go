package proxy

import (
	"fmt"
	"strings"

	"github.com/thapakazi/kazi/internal/labels"
)

// OverrideService describes one service's contribution to the compose override.
type OverrideService struct {
	Name     string
	Routable bool     // attach to the kazi network with Alias
	Alias    string   // <service>.<stack>
	Networks []string // service's existing networks from compose config; empty => ["default"]
	Ports    []string // kazi-added bindings "42017:5432" (from spec.expose)
}

// RenderOverride renders the compose override file that Up passes as an extra -f.
// kazi labels are always injected; kazi-network attachment (with alias) is added
// for routable services (preserving existing networks); port bindings are added
// when present. The top-level kazi network is declared external iff any service
// is routable.
func RenderOverride(stack string, svcs []OverrideService) []byte {
	var b strings.Builder

	anyRoutable := false
	for _, s := range svcs {
		if s.Routable {
			anyRoutable = true
			break
		}
	}

	b.WriteString("services:\n")
	for _, s := range svcs {
		fmt.Fprintf(&b, "  %q:\n", s.Name)

		// Labels block — always present.
		fmt.Fprintf(&b, "    labels:\n")
		fmt.Fprintf(&b, "      %s: \"true\"\n", labels.Managed)
		fmt.Fprintf(&b, "      %s: %q\n", labels.Stack, stack)

		// Networks block — only for routable services.
		if s.Routable {
			nets := s.Networks
			if len(nets) == 0 {
				nets = []string{"default"}
			}
			fmt.Fprintf(&b, "    networks:\n")
			for _, n := range nets {
				fmt.Fprintf(&b, "      %q: {}\n", n)
			}
			fmt.Fprintf(&b, "      %q:\n", "kazi")
			fmt.Fprintf(&b, "        aliases:\n")
			fmt.Fprintf(&b, "          - %q\n", s.Alias)
		}

		// Ports block — only when non-empty.
		if len(s.Ports) > 0 {
			fmt.Fprintf(&b, "    ports:\n")
			for _, p := range s.Ports {
				fmt.Fprintf(&b, "      - %q\n", p)
			}
		}
	}

	// Top-level networks block — only if any service is routable.
	if anyRoutable {
		fmt.Fprintf(&b, "networks:\n")
		fmt.Fprintf(&b, "  %q:\n", "kazi")
		fmt.Fprintf(&b, "    external: true\n")
	}

	return []byte(b.String())
}
