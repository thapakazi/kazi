package proxy

import (
	"testing"

	"github.com/thapakazi/kazi/internal/compose"
	"github.com/thapakazi/kazi/internal/store"
)

var httpPorts = []int{80, 3000, 8080}
var tcpPorts = []int{5432, 6379}

func svc(name string, ports ...int) compose.ServiceInfo {
	return compose.ServiceInfo{Name: name, Ports: ports}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		svc   compose.ServiceInfo
		class Class
		port  int
	}{
		{svc("web", 80), ClassHTTP, 80},
		{svc("api", 8080, 9229), ClassHTTP, 8080}, // known http port wins
		{svc("db", 5432), ClassTCP, 5432},
		{svc("cache", 6379, 5432), ClassTCP, 5432}, // all on tcp list, lowest
		{svc("app", 4321), ClassHTTP, 4321},        // single unknown port ⇒ http
		{svc("mixed", 4321, 9999), ClassNone, 0},   // multiple unknown ⇒ none
		{svc("worker"), ClassNone, 0},
	}
	for _, c := range cases {
		class, port := Classify(c.svc, httpPorts, tcpPorts)
		if class != c.class || port != c.port {
			t.Errorf("Classify(%s) = (%v,%d), want (%v,%d)", c.svc.Name, class, port, c.class, c.port)
		}
	}
}

func TestBuildPlanSingleHTTP(t *testing.T) {
	p := BuildPlan("blog", nil, []compose.ServiceInfo{svc("web", 80), svc("db", 5432)}, httpPorts, tcpPorts)
	if p.Primary != "web" || p.NeedsDecl {
		t.Errorf("plan = %+v", p)
	}
	if len(p.Routes) != 1 || p.Routes[0].Hostname != "blog.localhost" ||
		p.Routes[0].Alias != "web.blog" || p.Routes[0].Port != 80 {
		t.Errorf("routes = %+v", p.Routes)
	}
	if !p.Routable["web"] || p.Routable["db"] {
		t.Errorf("routable = %v", p.Routable)
	}
	if len(p.TCP) != 1 || p.TCP[0].Service != "db" || p.TCP[0].Port != 5432 {
		t.Errorf("tcp = %+v", p.TCP)
	}
}

func TestBuildPlanMultiHTTPNoDecl(t *testing.T) {
	p := BuildPlan("shop", nil, []compose.ServiceInfo{svc("web", 80), svc("api", 3000)}, httpPorts, tcpPorts)
	if !p.NeedsDecl || p.Primary != "" {
		t.Errorf("plan = %+v", p)
	}
	if len(p.Routes) != 2 {
		t.Fatalf("routes = %+v", p.Routes)
	}
	// no bare shop.localhost; both get subdomains, sorted by hostname
	if p.Routes[0].Hostname != "api.shop.localhost" || p.Routes[1].Hostname != "web.shop.localhost" {
		t.Errorf("routes = %+v", p.Routes)
	}
}

func TestBuildPlanDeclaredPrimary(t *testing.T) {
	decl := &store.ProxySpec{Service: "api", HTTPPort: 3000}
	p := BuildPlan("shop", decl, []compose.ServiceInfo{svc("web", 80), svc("api", 3000)}, httpPorts, tcpPorts)
	if p.Primary != "api" || p.NeedsDecl {
		t.Errorf("plan = %+v", p)
	}
	// api gets the bare hostname, web the subdomain
	if p.Routes[0].Hostname != "shop.localhost" || p.Routes[0].Service != "api" ||
		p.Routes[1].Hostname != "web.shop.localhost" {
		t.Errorf("routes = %+v", p.Routes)
	}
}

func TestBuildPlanDisabled(t *testing.T) {
	off := false
	p := BuildPlan("blog", &store.ProxySpec{Enabled: &off},
		[]compose.ServiceInfo{svc("web", 80)}, httpPorts, tcpPorts)
	if len(p.Routes) != 0 || len(p.Routable) != 0 || p.NeedsDecl {
		t.Errorf("disabled stack must have empty plan: %+v", p)
	}
}
