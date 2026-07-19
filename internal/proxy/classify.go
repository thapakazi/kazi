// Package proxy holds M1's routing logic: service classification,
// route planning, Caddyfile/override rendering, port allocation, and the
// kazi-proxy system-stack manager. Everything here is engine-internal;
// skins never import it.
package proxy

import (
	"slices"
	"sort"

	"github.com/thapakazi/kazi/internal/compose"
	"github.com/thapakazi/kazi/internal/store"
)

// Class describes how a service should be routed.
type Class int

const (
	ClassNone Class = iota
	ClassHTTP
	ClassTCP
)

// Route is one HTTP reverse-proxy entry.
type Route struct {
	Stack, Service string
	Hostname       string // blog.localhost or api.blog.localhost
	Alias          string // <service>.<stack> — the kazi-network dial target
	Port           int    // container HTTP port
}

// TCPService records a known-TCP service for nudge messages.
type TCPService struct {
	Service string
	Port    int // container port
}

// Plan is the complete routing decision for one stack.
type Plan struct {
	Routes    []Route         // HTTP routes (Caddyfile input), sorted by Hostname
	Routable  map[string]bool // service -> must join the kazi network
	Primary   string          // primary HTTP service ("" if none)
	NeedsDecl bool            // >1 HTTP service and no declaration: no bare hostname
	TCP       []TCPService    // known-TCP services (for urls/expose nudges), sorted by Service
}

// Classify decides HTTP/TCP/None for one service plus its HTTP port.
func Classify(svc compose.ServiceInfo, httpPorts, tcpPorts []int) (Class, int) {
	if len(svc.Ports) == 0 {
		return ClassNone, 0
	}
	httpSet, tcpSet := toSet(httpPorts), toSet(tcpPorts)

	// Find the lowest HTTP-list port, if any.
	lowestHTTP := 0
	for _, p := range svc.Ports {
		if httpSet[p] {
			if lowestHTTP == 0 || p < lowestHTTP {
				lowestHTTP = p
			}
		}
	}
	if lowestHTTP != 0 {
		return ClassHTTP, lowestHTTP
	}

	allTCP := true
	lowestTCP := 0
	for _, p := range svc.Ports {
		if !tcpSet[p] {
			allTCP = false
			break
		}
		if lowestTCP == 0 || p < lowestTCP {
			lowestTCP = p
		}
	}
	if allTCP {
		return ClassTCP, lowestTCP
	}
	if len(svc.Ports) == 1 {
		return ClassHTTP, svc.Ports[0]
	}
	return ClassNone, 0
}

func toSet(ps []int) map[int]bool {
	m := make(map[int]bool, len(ps))
	for _, p := range ps {
		m[p] = true
	}
	return m
}

// BuildPlan applies the spec's primary/subdomain rules for one stack.
// decl may be nil (undeclared / discovered stack).
func BuildPlan(stack string, decl *store.ProxySpec, svcs []compose.ServiceInfo, httpPorts, tcpPorts []int) Plan {
	plan := Plan{Routable: map[string]bool{}}
	if decl != nil && decl.Enabled != nil && !*decl.Enabled {
		return plan
	}
	httpPort := map[string]int{}
	var httpSvcs []string
	for _, s := range svcs {
		class, port := Classify(s, httpPorts, tcpPorts)
		switch class {
		case ClassHTTP:
			httpSvcs = append(httpSvcs, s.Name)
			httpPort[s.Name] = port
		case ClassTCP:
			plan.TCP = append(plan.TCP, TCPService{Service: s.Name, Port: port})
		}
	}
	sort.Strings(httpSvcs)
	sort.Slice(plan.TCP, func(i, j int) bool { return plan.TCP[i].Service < plan.TCP[j].Service })

	switch {
	case decl != nil && decl.Service != "":
		plan.Primary = decl.Service
		if decl.HTTPPort != 0 {
			httpPort[decl.Service] = decl.HTTPPort
		}
		if _, ok := httpPort[decl.Service]; !ok { // declared but unclassifiable: trust the declaration only with a port
			plan.Primary = ""
		} else if !slices.Contains(httpSvcs, decl.Service) {
			httpSvcs = append(httpSvcs, decl.Service)
			sort.Strings(httpSvcs)
		}
	case len(httpSvcs) == 1:
		plan.Primary = httpSvcs[0]
	case len(httpSvcs) > 1:
		plan.NeedsDecl = true
	}

	// hostBase is the *.localhost subdomain: a custom spec.proxy.hostname when
	// set, else the stack name. The internal network Alias always stays
	// stack-based (it's the container DNS name, not the public URL).
	hostBase := stack
	if decl != nil && decl.Hostname != "" {
		hostBase = decl.Hostname
	}
	for _, name := range httpSvcs {
		plan.Routable[name] = true
		host := name + "." + hostBase + ".localhost"
		if name == plan.Primary {
			host = hostBase + ".localhost"
		}
		plan.Routes = append(plan.Routes, Route{
			Stack: stack, Service: name, Hostname: host,
			Alias: name + "." + stack, Port: httpPort[name],
		})
	}
	sort.Slice(plan.Routes, func(i, j int) bool { return plan.Routes[i].Hostname < plan.Routes[j].Hostname })
	return plan
}
