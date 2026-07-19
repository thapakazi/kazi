package engine

import (
	"testing"

	"github.com/thapakazi/kazi/internal/labels"
	"github.com/thapakazi/kazi/internal/runtime"
)

func TestParsePublishedPorts(t *testing.T) {
	cases := []struct {
		in   string
		want []portMapping
	}{
		{"0.0.0.0:54323->3000/tcp", []portMapping{{54323, 3000}}},
		{"8080/tcp", nil}, // not published
		{"1025/tcp, 1110/tcp, 0.0.0.0:54324->8025/tcp", []portMapping{{54324, 8025}}},
		{"0.0.0.0:9000-9001->9000-9001/tcp", nil},                                       // range: non-numeric, skipped
		{"[::]:54323->3000/tcp, 0.0.0.0:54323->3000/tcp", []portMapping{{54323, 3000}}}, // dedup by host
		{"", nil},
	}
	for _, tc := range cases {
		got := parsePublishedPorts(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("parsePublishedPorts(%q) = %v, want %v", tc.in, got, tc.want)
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Fatalf("parsePublishedPorts(%q)[%d] = %v, want %v", tc.in, i, got[i], tc.want[i])
			}
		}
	}
}

func TestRouteHost(t *testing.T) {
	cases := []struct {
		base, stack, want string
	}{
		{"studio", "habrecare-mobile", "studio"},
		{"supabase_studio_habrecare-mobile", "habrecare-mobile", "supabase-studio"},
		{"MinIO", "sb", "minio"},
		{"", "sb", "sb"}, // empty falls back to the stack name
	}
	for _, tc := range cases {
		if got := routeHost(tc.base, tc.stack); got != tc.want {
			t.Errorf("routeHost(%q,%q) = %q, want %q", tc.base, tc.stack, got, tc.want)
		}
	}
}

func TestRouteAddListRemove(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := testEngine(t, &runtime.Fake{})

	if err := e.RouteAdd(t.Context(), "studio", 54323, "supabase", ""); err != nil {
		t.Fatal(err)
	}
	// Upsert: same host updates the port.
	if err := e.RouteAdd(t.Context(), "studio", 54999, "", ""); err != nil {
		t.Fatal(err)
	}
	routes, err := e.RouteList()
	if err != nil {
		t.Fatal(err)
	}
	if len(routes) != 1 || routes[0].Host != "studio" || routes[0].Port != 54999 {
		t.Fatalf("routes = %+v, want one studio→54999", routes)
	}

	// Invalid host / port are rejected.
	if err := e.RouteAdd(t.Context(), "Bad_Host", 80, "", ""); err == nil {
		t.Error("non-DNS-label host should be rejected")
	}
	if err := e.RouteAdd(t.Context(), "x", 99999, "", ""); err == nil {
		t.Error("out-of-range port should be rejected")
	}

	if err := e.RouteRemove(t.Context(), "studio"); err != nil {
		t.Fatal(err)
	}
	if routes, _ := e.RouteList(); len(routes) != 0 {
		t.Fatalf("routes after remove = %+v, want empty", routes)
	}
	if err := e.RouteRemove(t.Context(), "studio"); err == nil {
		t.Error("removing a missing route should error")
	}
}

func TestUrlsIncludesStaticRoutes(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	e := testEngine(t, &runtime.Fake{})
	// A route added "from" a (discovered, manifest-less) stack shows under it.
	if err := e.RouteAdd(t.Context(), "studio", 54323, "studio from sb", "sb"); err != nil {
		t.Fatal(err)
	}
	// An unscoped route only shows in the global listing.
	if err := e.RouteAdd(t.Context(), "adhoc", 8080, "", ""); err != nil {
		t.Fatal(err)
	}

	// Per-stack urls: only the sb route.
	eps, err := e.Urls(t.Context(), "sb")
	if err != nil {
		t.Fatal(err)
	}
	if len(eps) != 1 || eps[0].URL != "https://studio.localhost" || eps[0].Target != "host.docker.internal:54323" {
		t.Fatalf("urls(sb) = %+v, want the studio route", eps)
	}

	// Global urls: both routes present.
	all, err := e.Urls(t.Context(), "")
	if err != nil {
		t.Fatal(err)
	}
	var hosts []string
	for _, ep := range all {
		hosts = append(hosts, ep.Service)
	}
	if !containsStr(hosts, "studio") || !containsStr(hosts, "adhoc") {
		t.Fatalf("global urls should list both routes, got services %v", hosts)
	}
}

func containsStr(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

func TestRoutesFromStack(t *testing.T) {
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	f := &runtime.Fake{Containers: []runtime.Container{
		{Name: "supabase_studio_sb", Ports: "0.0.0.0:54323->3000/tcp", Labels: map[string]string{
			labels.ComposeProject: "sb", labels.ComposeService: "studio",
		}},
		{Name: "supabase_kong_sb", Ports: "8443/tcp, 0.0.0.0:54321->8000/tcp", Labels: map[string]string{
			labels.ComposeProject: "sb", labels.ComposeService: "kong",
		}},
		{Name: "supabase_db_sb", Ports: "5432/tcp", Labels: map[string]string{ // no published port
			labels.ComposeProject: "sb", labels.ComposeService: "db",
		}},
		{Name: "other", Ports: "0.0.0.0:9999->9999/tcp", Labels: map[string]string{
			labels.ComposeProject: "elsewhere",
		}},
	}}
	e := testEngine(t, f)

	cands, err := e.RoutesFromStack(t.Context(), "sb")
	if err != nil {
		t.Fatal(err)
	}
	// studio (54321? no, sorted by host port): kong 54321 then studio 54323. db skipped, other skipped.
	if len(cands) != 2 {
		t.Fatalf("want 2 candidates (published ports only, this stack only), got %+v", cands)
	}
	if cands[0].Host != "kong" || cands[0].Port != 54321 || cands[0].Target != 8000 {
		t.Errorf("cands[0] = %+v, want kong 54321→8000", cands[0])
	}
	if cands[1].Host != "studio" || cands[1].Port != 54323 {
		t.Errorf("cands[1] = %+v, want studio 54323", cands[1])
	}
}
