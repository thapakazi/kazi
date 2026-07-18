// Package runtime abstracts the container CLI (docker first-class;
// podman/nerdctl best-effort). Everything is subprocess-based — kazi
// never opens the Docker API socket.
package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"

	"github.com/thapakazi/kazi/internal/labels"
)

var ErrNoRuntime = errors.New("no container runtime found (tried docker, podman, nerdctl)")

type Container struct {
	ID     string
	Name   string
	Image  string
	State  string // running, exited, created, ...
	Status string // human string, e.g. "Up 3 hours (healthy)"
	Ports  string
	Labels map[string]string
}

type Runtime interface {
	Name() string
	Ps(ctx context.Context) ([]Container, error)
	// ComposeCmd builds `<bin> compose -p <project> --project-directory <dir>
	// [-f file]... <args...>` without running it.
	ComposeCmd(ctx context.Context, project, dir string, files []string, args ...string) *exec.Cmd
}

// Detect picks a runtime. pref "" or "auto" probes docker, podman, nerdctl
// in order; an explicit name probes only that binary.
func Detect(pref string) (Runtime, error) {
	order := []string{"docker", "podman", "nerdctl"}
	if pref != "" && pref != "auto" {
		order = []string{pref}
	}
	for _, bin := range order {
		if _, err := exec.LookPath(bin); err == nil {
			return &CLI{Bin: bin}, nil
		}
	}
	return nil, ErrNoRuntime
}

type CLI struct {
	Bin string
}

func (c *CLI) Name() string { return c.Bin }

func (c *CLI) Ps(ctx context.Context) ([]Container, error) {
	// --no-trunc is added beyond the spec's `ps -a --format json` to get full
	// container IDs and untruncated label values; truncated label CSV would
	// break key=value parsing (a label value containing "," would be cut off).
	cmd := exec.CommandContext(ctx, c.Bin, "ps", "-a", "--no-trunc", "--format", "json")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s ps failed: %v: %s", c.Bin, err, stderr.String())
	}
	return parsePs(out)
}

// psRow matches docker's line-delimited `ps --format json` output.
// nerdctl emits the same shape; podman is best-effort.
type psRow struct {
	ID     string `json:"ID"`
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	State  string `json:"State"`
	Status string `json:"Status"`
	Ports  string `json:"Ports"`
	Labels string `json:"Labels"`
}

func parsePs(out []byte) ([]Container, error) {
	var cs []Container
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 1024*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r psRow
		if err := json.Unmarshal(line, &r); err != nil {
			return nil, fmt.Errorf("parsing ps output line %q: %w", line, err)
		}
		cs = append(cs, Container{
			ID: r.ID, Name: r.Names, Image: r.Image,
			State: r.State, Status: r.Status, Ports: r.Ports,
			Labels: labels.ParseDockerCSV(r.Labels),
		})
	}
	return cs, sc.Err()
}

func (c *CLI) ComposeCmd(ctx context.Context, project, dir string, files []string, args ...string) *exec.Cmd {
	a := []string{"compose", "-p", project, "--project-directory", dir}
	for _, f := range files {
		a = append(a, "-f", f)
	}
	a = append(a, args...)
	return exec.CommandContext(ctx, c.Bin, a...)
}
