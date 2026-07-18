package runtime

import (
	"errors"
	"strings"
	"testing"
)

// docker ps -a --format json emits one JSON object per line.
const dockerPsLines = `{"ID":"aaa","Names":"blog-web-1","Image":"nginx:alpine","State":"running","Status":"Up 3 hours (healthy)","Ports":"0.0.0.0:8080->80/tcp","Labels":"com.docker.compose.project=blog,com.docker.compose.service=web,com.docker.compose.project.working_dir=/tmp/blog"}
{"ID":"bbb","Names":"lonely","Image":"redis:7","State":"exited","Status":"Exited (0) 2 days ago","Ports":"","Labels":""}`

func TestParsePsLines(t *testing.T) {
	cs, err := parsePs([]byte(dockerPsLines))
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("got %d containers", len(cs))
	}
	web := cs[0]
	if web.Name != "blog-web-1" || web.State != "running" {
		t.Errorf("web = %+v", web)
	}
	if web.Labels["com.docker.compose.project"] != "blog" {
		t.Errorf("labels = %v", web.Labels)
	}
	if !strings.Contains(web.Ports, "8080") {
		t.Errorf("ports = %q", web.Ports)
	}
	if len(cs[1].Labels) != 0 {
		t.Errorf("lonely labels = %v", cs[1].Labels)
	}
}

func TestParsePsEmpty(t *testing.T) {
	cs, err := parsePs([]byte("\n"))
	if err != nil || len(cs) != 0 {
		t.Errorf("cs=%v err=%v", cs, err)
	}
}

func TestDetectUnknownRuntime(t *testing.T) {
	if _, err := Detect("no-such-binary-xyz"); !errors.Is(err, ErrNoRuntime) {
		t.Errorf("want ErrNoRuntime, got %v", err)
	}
}

func TestCLIComposeCmd(t *testing.T) {
	c := &CLI{Bin: "docker"}
	cmd := c.ComposeCmd(t.Context(), "kazi-blog", "/tmp/blog", []string{"/tmp/blog/compose.yml", "/tmp/ov.yml"}, "up", "-d")
	want := []string{"docker", "compose", "-p", "kazi-blog", "--project-directory", "/tmp/blog",
		"-f", "/tmp/blog/compose.yml", "-f", "/tmp/ov.yml", "up", "-d"}
	if strings.Join(cmd.Args, " ") != strings.Join(want, " ") {
		t.Errorf("args = %v\nwant %v", cmd.Args, want)
	}
}

func TestCLICmd(t *testing.T) {
	c := &CLI{Bin: "docker"}
	cmd := c.Cmd(t.Context(), "network", "create", "kazi")
	want := "docker network create kazi"
	if strings.Join(cmd.Args, " ") != want {
		t.Errorf("args = %v, want %q", cmd.Args, want)
	}
}

func TestFakeCmdScripting(t *testing.T) {
	f := &Fake{
		FailPrefix: []string{"network inspect"},
		CmdOut:     map[string]string{"exec kazi-proxy cat": "CERTDATA"},
	}
	if err := f.Cmd(t.Context(), "network", "inspect", "kazi").Run(); err == nil {
		t.Error("inspect should fail (scripted)")
	}
	out, err := f.Cmd(t.Context(), "exec", "kazi-proxy", "cat", "/data/root.crt").Output()
	if err != nil || strings.TrimSpace(string(out)) != "CERTDATA" {
		t.Errorf("out=%q err=%v", out, err)
	}
	if err := f.Cmd(t.Context(), "network", "create", "kazi").Run(); err != nil {
		t.Errorf("create should succeed: %v", err)
	}
	if len(f.Cmds) != 3 || strings.Join(f.Cmds[2], " ") != "network create kazi" {
		t.Errorf("cmds = %v", f.Cmds)
	}
}

func TestFakeComposeConfigJSON(t *testing.T) {
	f := &Fake{ConfigJSON: `{"services":{}}`}
	out, err := f.ComposeCmd(t.Context(), "p", "/d", nil, "config", "--format", "json").Output()
	if err != nil || strings.TrimSpace(string(out)) != `{"services":{}}` {
		t.Errorf("out=%q err=%v", out, err)
	}
}
