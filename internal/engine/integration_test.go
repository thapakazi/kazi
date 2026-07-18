//go:build integration

package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/thapakazi/kazi/internal/proxy"
	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

// TestComposeLifecycle drives a real compose project end to end against
// Docker: add -> up -> status/ps (labels, discovery) -> down.
// Run with: go test -tags integration ./internal/engine/ -v -run Lifecycle
func TestComposeLifecycle(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())

	rt, err := runtime.Detect("docker")
	if err != nil {
		t.Fatal(err)
	}
	cfg, cfgErr := store.LoadConfig()
	if cfgErr != nil {
		t.Fatal(cfgErr)
	}
	e := New(rt, cfg, os.Stdout, os.Stderr)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	fixture, err := filepath.Abs("testdata/fixture")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Add("itest", fixture); err != nil {
		t.Fatal(err)
	}
	defer e.Down(ctx, "itest") // cleanup even on failure

	if err := e.Up(ctx, "itest"); err != nil {
		t.Fatal(err)
	}
	st, err := e.Status(ctx, "itest")
	if err != nil {
		t.Fatal(err)
	}
	if st.Kind != KindRegistered || st.Running < 1 {
		t.Errorf("status = %+v", st)
	}

	// kazi labels were injected via the override file.
	cs, err := e.Ps(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range cs {
		if c.Stack == "itest" && c.Kind == KindRegistered {
			found = true
		}
	}
	if !found {
		t.Errorf("itest container not grouped as registered: %+v", cs)
	}

	if err := e.Down(ctx, "itest"); err != nil {
		t.Fatal(err)
	}
}

// itestEngine returns a new Engine wired to a fresh temp KAZI_CONFIG_DIR.
// It also sets the env var so store.Root() works properly for this test.
func itestEngine(t *testing.T) *Engine {
	t.Helper()
	t.Setenv("KAZI_CONFIG_DIR", t.TempDir())
	rt, err := runtime.Detect("docker")
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	return New(rt, cfg, os.Stdout, os.Stderr)
}

// cleanupProxy tears down the kazi-proxy compose project and the kazi network.
// Best-effort: logs errors but does not fail the test.
func cleanupProxy(t *testing.T, proxyDir string) {
	t.Helper()
	if proxyDir == "" {
		proxyDir = proxy.Dir()
	}
	cmd := exec.Command("docker", "compose", "-p", "kazi-proxy",
		"--project-directory", proxyDir, "down", "-v")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("cleanup: kazi-proxy down: %v\n%s", err, out)
	}
	netCmd := exec.Command("docker", "network", "rm", "kazi")
	netOut, netErr := netCmd.CombinedOutput()
	if netErr != nil {
		t.Logf("cleanup: network rm kazi: %v\n%s", netErr, netOut)
	}
}

// TestProxyRoutingTwoStacks adds two stacks (alpha, beta) with identically-named
// web services, brings both up, and verifies:
//  1. The kazi network exists (created by EnsureNetwork on Up).
//  2. The Caddyfile contains alpha.localhost and beta.localhost with the correct
//     reverse_proxy aliases (web.alpha:80, web.beta:80) — this is always tested.
//  3. If kazi-proxy is running (ports 80/443 free), also probe HTTP via host curl
//     (preferred) or docker exec wget (fallback) and verify the two stacks route
//     to distinct container hostnames.  If ports are busy the proxy won't start;
//     in that case the test logs the reason and skips HTTP probing.
//  4. Down both stacks → Caddyfile no longer contains either hostname.
func TestProxyRoutingTwoStacks(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}

	e := itestEngine(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	m1aFixture, err := filepath.Abs("testdata/m1a")
	if err != nil {
		t.Fatal(err)
	}
	m1bFixture, err := filepath.Abs("testdata/m1b")
	if err != nil {
		t.Fatal(err)
	}

	// Register stacks alpha and beta.
	if _, err := e.Add("alpha", m1aFixture); err != nil {
		t.Fatal("add alpha:", err)
	}
	defer func() {
		_ = e.Down(ctx, "alpha")
		_ = e.Remove("alpha")
	}()

	if _, err := e.Add("beta", m1bFixture); err != nil {
		t.Fatal("add beta:", err)
	}
	defer func() {
		_ = e.Down(ctx, "beta")
		_ = e.Remove("beta")
	}()
	defer cleanupProxy(t, proxy.Dir())

	// Bring both stacks up. Warnings about proxy sync (port 80/443 busy on host)
	// are written to stderr but Up itself must succeed.
	if err := e.Up(ctx, "alpha"); err != nil {
		t.Fatal("up alpha:", err)
	}
	if err := e.Up(ctx, "beta"); err != nil {
		t.Fatal("up beta:", err)
	}

	// 1. kazi network must exist (created by EnsureNetwork).
	netOut, netErr := exec.CommandContext(ctx, "docker", "network", "inspect", "kazi").CombinedOutput()
	if netErr != nil {
		t.Fatalf("kazi network not found: %v\n%s", netErr, netOut)
	}
	t.Logf("kazi network exists")

	// 2. Check Caddyfile content — always verified regardless of proxy state.
	caddyPath := filepath.Join(proxy.Dir(), "Caddyfile")
	caddyBytes, err := os.ReadFile(caddyPath)
	if err != nil {
		t.Fatalf("reading Caddyfile: %v", err)
	}
	caddyContent := string(caddyBytes)
	t.Logf("Caddyfile:\n%s", caddyContent)

	checks := []struct{ needle, desc string }{
		{"alpha.localhost", "alpha.localhost route"},
		{"beta.localhost", "beta.localhost route"},
		{"web.alpha:80", "web.alpha:80 reverse_proxy"},
		{"web.beta:80", "web.beta:80 reverse_proxy"},
	}
	for _, c := range checks {
		if !strings.Contains(caddyContent, c.needle) {
			t.Errorf("Caddyfile missing %s: %q", c.desc, c.needle)
		}
	}

	// 3. HTTP probing: conditional on proxy running.
	psOut, _ := exec.CommandContext(ctx, "docker", "ps", "--filter", "name=kazi-proxy", "--format", "{{.Names}}").Output()
	proxyRunning := strings.Contains(string(psOut), "kazi-proxy")

	if !proxyRunning {
		t.Logf("kazi-proxy not running (likely ports 80/443 busy on host); skipping HTTP probe — Caddyfile assertions above cover routing correctness")
	} else {
		t.Logf("kazi-proxy is running; probing HTTP endpoints")
		alphaBody := probeHTTP(ctx, t, "alpha")
		betaBody := probeHTTP(ctx, t, "beta")

		if !strings.Contains(alphaBody, "Hostname:") {
			t.Errorf("alpha response missing Hostname: line; got: %q", alphaBody)
		}
		if !strings.Contains(betaBody, "Hostname:") {
			t.Errorf("beta response missing Hostname: line; got: %q", betaBody)
		}
		// Confirm the two container hostnames differ.
		alphaHost := extractHostname(alphaBody)
		betaHost := extractHostname(betaBody)
		if alphaHost != "" && betaHost != "" && alphaHost == betaHost {
			t.Errorf("alpha and beta returned the same Hostname (%q) — routing may be broken", alphaHost)
		}
	}

	// 4. Down both stacks; Caddyfile must no longer contain their hostnames.
	if err := e.Down(ctx, "alpha"); err != nil {
		t.Fatal("down alpha:", err)
	}
	if err := e.Down(ctx, "beta"); err != nil {
		t.Fatal("down beta:", err)
	}

	afterBytes, err := os.ReadFile(caddyPath)
	if err != nil {
		t.Fatalf("reading Caddyfile after down: %v", err)
	}
	after := string(afterBytes)
	if strings.Contains(after, "alpha.localhost") {
		t.Errorf("Caddyfile still contains alpha.localhost after down:\n%s", after)
	}
	if strings.Contains(after, "beta.localhost") {
		t.Errorf("Caddyfile still contains beta.localhost after down:\n%s", after)
	}
}

// probeHTTP tries host curl first; on failure (e.g. ports 80/443 busy),
// falls back to docker exec wget inside the kazi-proxy container.
// Returns the response body or "" on complete failure (logged, not fatal).
func probeHTTP(ctx context.Context, t *testing.T, stack string) string {
	t.Helper()
	host := stack + ".localhost"
	url := "https://" + host

	// Try host curl (ports 443 likely free in CI, but may be busy on a dev machine).
	curlOut, curlErr := exec.CommandContext(ctx,
		"curl", "-sk", "--resolve", fmt.Sprintf("%s:443:127.0.0.1", host),
		url,
	).Output()
	if curlErr == nil && len(curlOut) > 0 {
		t.Logf("[curl] %s response: %s", host, curlOut)
		return string(curlOut)
	}
	t.Logf("[curl] %s failed (%v); falling back to docker exec wget", host, curlErr)

	// Fall back: wget inside the proxy container using the Host header.
	wgetOut, wgetErr := exec.CommandContext(ctx,
		"docker", "exec", "kazi-proxy",
		"wget", "-qO-", "--header", "Host: "+host, "http://localhost",
	).Output()
	if wgetErr == nil {
		t.Logf("[docker exec wget] %s response: %s", host, wgetOut)
		return string(wgetOut)
	}
	t.Logf("[docker exec wget] %s failed: %v\n%s", host, wgetErr, wgetOut)
	return ""
}

// extractHostname pulls the value after "Hostname:" from a whoami response.
func extractHostname(body string) string {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "Hostname:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "Hostname:"))
		}
	}
	return ""
}

// TestExposeRoundTripIntegration verifies the Expose verb's full lifecycle:
//  1. Add stack gamma; Expose web with port 0 (auto-detect port 80 from compose expose:).
//  2. The returned port P is in 42000-42999.
//  3. Up gamma; docker ps shows 0.0.0.0:P->80/tcp on kazi-gamma_web container.
//  4. Down gamma; Up gamma again → port is still P (state survives down/up).
//  5. Expose web with remove=true; Up gamma again → port binding gone.
//  6. Remove gamma.
//
// Note: the test uses the "web" service (traefik/whoami with expose: ["80"]) so
// that port auto-detection succeeds.  The "db" service (alpine sleep) has no
// declared ports and cannot be used with port=0.
func TestExposeRoundTripIntegration(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}

	e := itestEngine(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	m1aFixture, err := filepath.Abs("testdata/m1a")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.Add("gamma", m1aFixture); err != nil {
		t.Fatal("add gamma:", err)
	}
	defer func() {
		_ = e.Down(ctx, "gamma")
		_ = e.Remove("gamma")
		cleanupProxy(t, proxy.Dir())
	}()

	// Expose the web service with port=0 so Expose auto-detects the container port
	// (port 80, declared via expose: in the compose file).
	// Allocate returns a host port in [42000, 42999].
	port1, expErr := e.Expose(ctx, "gamma", "web", 0, false)
	if expErr != nil {
		t.Fatal("expose gamma/web:", expErr)
	}
	if port1 < 42000 || port1 > 42999 {
		t.Fatalf("exposed port %d not in 42000-42999", port1)
	}
	t.Logf("expose allocated port %d for gamma/web:80", port1)

	// Up gamma → web container should have port binding.
	if err := e.Up(ctx, "gamma"); err != nil {
		t.Fatal("up gamma:", err)
	}

	// Check docker ps for the port binding.
	assertPortBinding(ctx, t, "kazi-gamma", "web", port1)

	// Down and Up again — port must be the same (state survives).
	if err := e.Down(ctx, "gamma"); err != nil {
		t.Fatal("down gamma:", err)
	}
	if err := e.Up(ctx, "gamma"); err != nil {
		t.Fatal("up gamma (second):", err)
	}

	port2, expErr2 := e.Expose(ctx, "gamma", "web", 0, false)
	if expErr2 != nil {
		t.Fatal("re-expose gamma/web:", expErr2)
	}
	if port2 != port1 {
		t.Errorf("port changed after down/up: was %d, now %d (state must survive down/up)", port1, port2)
	}
	assertPortBinding(ctx, t, "kazi-gamma", "web", port1)

	// Expose --remove; Down + Up again → port binding gone.
	if _, removeErr := e.Expose(ctx, "gamma", "web", 0, true); removeErr != nil {
		t.Fatal("expose --remove gamma/web:", removeErr)
	}
	if err := e.Down(ctx, "gamma"); err != nil {
		t.Fatal("down gamma (pre-remove check):", err)
	}
	if err := e.Up(ctx, "gamma"); err != nil {
		t.Fatal("up gamma (after remove):", err)
	}

	// Confirm port is gone.
	psOut, err := exec.CommandContext(ctx, "docker", "ps",
		"--filter", "label=com.docker.compose.project=kazi-gamma",
		"--filter", "label=com.docker.compose.service=web",
		"--format", "{{.Ports}}",
	).Output()
	if err != nil {
		t.Fatalf("docker ps after expose --remove: %v", err)
	}
	portStr := fmt.Sprintf("%d->", port1)
	if strings.Contains(string(psOut), portStr) {
		t.Errorf("port %d still bound after expose --remove; docker ps: %s", port1, psOut)
	}
	t.Logf("port %d correctly absent after expose --remove", port1)
}

// assertPortBinding checks that the docker ps output for project/service contains
// "hostPort->".
func assertPortBinding(ctx context.Context, t *testing.T, project, service string, hostPort int) {
	t.Helper()
	out, err := exec.CommandContext(ctx, "docker", "ps",
		"--filter", "label=com.docker.compose.project="+project,
		"--filter", "label=com.docker.compose.service="+service,
		"--format", "{{.Ports}}",
	).Output()
	if err != nil {
		t.Fatalf("docker ps for %s/%s: %v", project, service, err)
	}
	portStr := fmt.Sprintf("%d->", hostPort)
	if !strings.Contains(string(out), portStr) {
		t.Errorf("port binding %s not found in docker ps output %q for %s/%s", portStr, string(out), project, service)
	}
	t.Logf("confirmed port binding %s for %s/%s", portStr, project, service)
}

// TestProxyKilledDuringUp verifies that kazi's Up is resilient to a dead
// kazi-proxy container: Up returns nil (no error), and either the proxy was
// restarted by Sync (preferred, when ports 80/443 are free) or stderr contains
// a "kazi: warning:" line (acceptable when ports are busy on this host).
func TestProxyKilledDuringUp(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not on PATH")
	}

	e := itestEngine(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	m1aFixture, err := filepath.Abs("testdata/m1a")
	if err != nil {
		t.Fatal(err)
	}
	m1bFixture, err := filepath.Abs("testdata/m1b")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := e.Add("alpha", m1aFixture); err != nil {
		t.Fatal("add alpha:", err)
	}
	defer func() {
		_ = e.Down(ctx, "alpha")
		_ = e.Remove("alpha")
	}()

	if _, err := e.Add("beta", m1bFixture); err != nil {
		t.Fatal("add beta:", err)
	}
	defer func() {
		_ = e.Down(ctx, "beta")
		_ = e.Remove("beta")
		cleanupProxy(t, proxy.Dir())
	}()

	// Bring alpha up. Warnings about proxy sync failure are expected on machines
	// where ports 80/443 are already bound (written to os.Stderr, not captured).
	if err := e.Up(ctx, "alpha"); err != nil {
		t.Fatal("up alpha:", err)
	}

	// Check if proxy is running. If so, kill it to set up the killed-during-up scenario.
	// If not (ports busy), we proceed directly — the next Up will still exercise
	// the "proxy down → Sync attempts restart → warning if it fails" code path.
	psOutBefore, _ := exec.CommandContext(ctx, "docker", "ps", "--filter", "name=kazi-proxy", "--format", "{{.Names}}").Output()
	if strings.Contains(string(psOutBefore), "kazi-proxy") {
		rmOut, rmErr := exec.CommandContext(ctx, "docker", "rm", "-f", "kazi-proxy").CombinedOutput()
		if rmErr != nil {
			t.Fatalf("docker rm -f kazi-proxy: %v\n%s", rmErr, rmOut)
		}
		t.Logf("kazi-proxy was running; forcibly removed to simulate kill-during-up")
	} else {
		t.Logf("kazi-proxy was not running (ports 80/443 likely busy on host); exercising warning path directly")
	}

	// Now Up beta with stderr captured separately so we can inspect it.
	var stderrBuf bytes.Buffer
	eBeta := New(e.RT, e.Cfg, os.Stdout, &stderrBuf)
	upErr := eBeta.Up(ctx, "beta")

	// Up must return nil regardless of proxy state — this is the key invariant.
	if upErr != nil {
		t.Fatalf("Up(beta) returned error when proxy was dead: %v", upErr)
	}

	stderrOutput := stderrBuf.String()
	t.Logf("stderr from Up(beta): %q", stderrOutput)

	// Assert: either proxy restarted (preferred) or a warning was emitted (acceptable).
	psOutAfter, _ := exec.CommandContext(ctx, "docker", "ps", "--filter", "name=kazi-proxy", "--format", "{{.Names}}").Output()
	proxyRestarted := strings.Contains(string(psOutAfter), "kazi-proxy")

	if proxyRestarted {
		t.Logf("PASS path A: kazi-proxy restarted by Sync (preferred — ports 80/443 were free)")
	} else if strings.Contains(stderrOutput, "kazi: warning:") {
		t.Logf("PASS path B: kazi: warning: emitted (acceptable — ports 80/443 busy on host)")
	} else {
		t.Errorf("FAIL: neither proxy restarted nor warning emitted; proxyRunning=%v stderr=%q",
			proxyRestarted, stderrOutput)
	}
}
