package engine

import (
	"context"
	"fmt"
)

// HostStats is the host-level resource sample rendered on the TUI's ALL
// overview and returned by `kazi stats --host`. CPUPercent is summed across
// cores (ONCE-style: can exceed 100%); mem/disk are absolute bytes. Host stats
// come from a portable OS read (gopsutil), never the Docker API socket.
type HostStats struct {
	CPUPercent float64 `json:"cpuPercent"`
	CPUCores   int     `json:"cpuCores"`
	MemUsed    uint64  `json:"memUsed"`
	MemTotal   uint64  `json:"memTotal"`
	DiskUsed   uint64  `json:"diskUsed"`
	DiskTotal  uint64  `json:"diskTotal"`
}

// hostReading is a raw host sample before mapping/aggregation. Per-metric errors
// let HostStats degrade one metric (e.g. hide Disk on an exotic FS) without
// failing the whole overview.
type hostReading struct {
	perCPU    []float64 // per-core busy percentages
	cores     int
	memUsed   uint64
	memTotal  uint64
	diskUsed  uint64
	diskTotal uint64

	cpuErr  error
	memErr  error
	diskErr error
}

// hostProvider reads the raw host sample; the default is gopsutil-backed, tests
// inject a fake.
type hostProvider interface {
	read(ctx context.Context) hostReading
}

// HostStats reads host CPU/mem/disk via the provider (gopsutil by default),
// summing per-core CPU. It degrades per metric — a single failed metric zeroes
// just that pair — and returns an error only when every metric failed, so a
// wholly unsupported platform is flagged (doctor can note it) while a missing
// disk merely hides that block.
func (e *Engine) HostStats(ctx context.Context) (HostStats, error) {
	p := e.host
	if p == nil {
		p = gopsutilHost{}
	}
	r := p.read(ctx)

	var hs HostStats
	if r.cpuErr == nil {
		for _, v := range r.perCPU {
			hs.CPUPercent += v
		}
		hs.CPUCores = r.cores
	}
	if r.memErr == nil {
		hs.MemUsed, hs.MemTotal = r.memUsed, r.memTotal
	}
	if r.diskErr == nil {
		hs.DiskUsed, hs.DiskTotal = r.diskUsed, r.diskTotal
	}
	if r.cpuErr != nil && r.memErr != nil && r.diskErr != nil {
		return hs, fmt.Errorf("host stats unavailable: %v", r.cpuErr)
	}
	return hs, nil
}
