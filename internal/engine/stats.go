package engine

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ContainerStats is one container's live resource sample — the `--json` wire
// shape shared by `kazi stats`, MCP (later), and the TUI Stats tab. Byte
// quantities keep docker's human forms, split from the combined columns docker
// emits (MemUsage "used / limit", NetIO "rx / tx", BlockIO "read / write"); CPU
// and mem percentages are numeric so the TUI can sparkline them. Container stats
// come only from `<runtime> stats` — never the API socket.
type ContainerStats struct {
	Stack      string  `json:"stack,omitempty"`
	Service    string  `json:"service,omitempty"`
	Name       string  `json:"name"`
	CPUPercent float64 `json:"cpuPercent"`
	MemUsage   string  `json:"memUsage"`
	MemLimit   string  `json:"memLimit"`
	MemPercent float64 `json:"memPercent"`
	NetRx      string  `json:"netRx"`
	NetTx      string  `json:"netTx"`
	BlockRead  string  `json:"blockRead"`
	BlockWrite string  `json:"blockWrite"`
	PIDs       int     `json:"pids"`
	ID         string  `json:"id,omitempty"`
}

// StatSample is one streamed ContainerStats plus a monotonic sequence index, so
// the TUI can order/ring-buffer samples off a StatsStream.
type StatSample struct {
	ContainerStats
	Seq int `json:"seq"`
}

// statsRow matches docker's `stats --format json` line shape. nerdctl emits the
// same fields; podman is best-effort.
type statsRow struct {
	ID       string `json:"ID"`
	Name     string `json:"Name"`
	CPUPerc  string `json:"CPUPerc"`
	MemUsage string `json:"MemUsage"`
	MemPerc  string `json:"MemPerc"`
	NetIO    string `json:"NetIO"`
	BlockIO  string `json:"BlockIO"`
	PIDs     string `json:"PIDs"`
}

// Stats returns a one-shot resource snapshot for a stack's running containers,
// or every kazi-visible stack's running containers when stack is empty. It
// resolves container names from the passive ps snapshot, runs a single
// `<runtime> stats --no-stream` over their IDs, and attaches stack/service. A
// non-empty stack that doesn't exist is ErrStackNotFound; a runtime without JSON
// stats surfaces a wrapped error rather than crashing.
func (e *Engine) Stats(ctx context.Context, stack string) ([]ContainerStats, error) {
	stacks, _, err := e.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	// meta lets us re-attach stack/service after parsing, matched by container
	// name (docker stats emits short IDs, so name is the stable join key).
	type meta struct{ stack, service string }
	byName := map[string]meta{}
	var ids []string
	found := false
	for _, s := range stacks {
		if stack != "" && s.Name != stack {
			continue
		}
		found = true
		for _, c := range s.Containers {
			if c.State != "running" || c.ID == "" {
				continue
			}
			svc := c.Service
			if svc == "" {
				svc = c.Name
			}
			byName[c.Name] = meta{stack: s.Name, service: svc}
			ids = append(ids, c.ID)
		}
	}
	if stack != "" && !found {
		return nil, fmt.Errorf("%w: %s", ErrStackNotFound, stack)
	}
	if len(ids) == 0 {
		return nil, nil
	}

	out, err := e.runStats(ctx, ids)
	if err != nil {
		return nil, err
	}
	stats, err := parseStats(out)
	if err != nil {
		return nil, err
	}
	for i := range stats {
		if m, ok := byName[stats[i].Name]; ok {
			stats[i].Stack = m.stack
			stats[i].Service = m.service
		}
	}
	return stats, nil
}

// StatsStream follows `<runtime> stats --format json` over the given container
// IDs, emitting one StatSample per container per interval on the returned
// channel until ctx is cancelled or the stream ends (then the channel closes).
// Empty ids yields an already-closed channel. Malformed lines are skipped. The
// caller (TUI) owns cancellation via ctx and ring-buffers the samples.
func (e *Engine) StatsStream(ctx context.Context, ids []string) (<-chan StatSample, error) {
	ch := make(chan StatSample)
	if len(ids) == 0 {
		close(ch)
		return ch, nil
	}
	cmd := e.RT.StatsCmd(ctx, ids, true)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("%s stats stream failed: %w", e.RT.Name(), err)
	}
	go func() {
		defer close(ch)
		defer func() { _ = cmd.Wait() }()
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		seq := 0
		for sc.Scan() {
			line := bytes.TrimSpace(sc.Bytes())
			if len(line) == 0 {
				continue
			}
			cs, perr := parseStatsLine(line)
			if perr != nil {
				continue
			}
			select {
			case ch <- StatSample{ContainerStats: cs, Seq: seq}:
			case <-ctx.Done():
				return
			}
			seq++
		}
	}()
	return ch, nil
}

// runStats executes the one-shot stats command, capturing stderr so a runtime
// that lacks JSON stats returns a structured error instead of an opaque crash.
func (e *Engine) runStats(ctx context.Context, ids []string) ([]byte, error) {
	cmd := e.RT.StatsCmd(ctx, ids, false)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s stats failed: %v: %s", e.RT.Name(), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

// parseStats parses newline-delimited docker-stats JSON into ContainerStats.
func parseStats(out []byte) ([]ContainerStats, error) {
	var cs []ContainerStats
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		c, err := parseStatsLine(line)
		if err != nil {
			return nil, err
		}
		cs = append(cs, c)
	}
	return cs, sc.Err()
}

// parseStatsLine maps one docker-stats JSON object to a ContainerStats, splitting
// the combined mem/net/block columns and parsing the percentage/PID scalars.
func parseStatsLine(line []byte) (ContainerStats, error) {
	var r statsRow
	if err := json.Unmarshal(line, &r); err != nil {
		return ContainerStats{}, fmt.Errorf("parsing stats output line %q: %w", line, err)
	}
	memUsage, memLimit := splitSlashPair(r.MemUsage)
	netRx, netTx := splitSlashPair(r.NetIO)
	blkRead, blkWrite := splitSlashPair(r.BlockIO)
	return ContainerStats{
		ID:         r.ID,
		Name:       r.Name,
		CPUPercent: parsePercent(r.CPUPerc),
		MemUsage:   memUsage,
		MemLimit:   memLimit,
		MemPercent: parsePercent(r.MemPerc),
		NetRx:      netRx,
		NetTx:      netTx,
		BlockRead:  blkRead,
		BlockWrite: blkWrite,
		PIDs:       parseIntField(r.PIDs),
	}, nil
}

// splitSlashPair splits docker's "A / B" combined columns; a missing separator
// leaves the second half empty.
func splitSlashPair(s string) (a, b string) {
	if x, y, ok := strings.Cut(s, "/"); ok {
		return strings.TrimSpace(x), strings.TrimSpace(y)
	}
	return strings.TrimSpace(s), ""
}

// parsePercent turns "2.43%" into 2.43; a blank or unparseable value is 0.
func parsePercent(s string) float64 {
	s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "%"))
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return f
}

// parseIntField turns a PID count string into an int; blank/unparseable is 0.
func parseIntField(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}
