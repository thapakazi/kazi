package engine

import (
	"context"

	"github.com/shirou/gopsutil/v4/cpu"
	"github.com/shirou/gopsutil/v4/disk"
	"github.com/shirou/gopsutil/v4/mem"
)

// gopsutilHost is the default hostProvider: a portable (darwin/linux, no cgo)
// OS read via gopsutil. CPU uses interval 0 (non-blocking: the delta since the
// last call), so the first read is a since-boot average that self-corrects as
// the TUI polls. Disk is the root filesystem.
type gopsutilHost struct{}

func (gopsutilHost) read(ctx context.Context) hostReading {
	var r hostReading
	if p, err := cpu.PercentWithContext(ctx, 0, true); err != nil {
		r.cpuErr = err
	} else {
		r.perCPU = p
		r.cores = len(p)
	}
	// Prefer the logical core count when available; fall back to the per-core
	// sample length set above.
	if n, err := cpu.CountsWithContext(ctx, true); err == nil && n > 0 {
		r.cores = n
	}
	if vm, err := mem.VirtualMemoryWithContext(ctx); err != nil {
		r.memErr = err
	} else {
		r.memUsed, r.memTotal = vm.Used, vm.Total
	}
	if du, err := disk.UsageWithContext(ctx, "/"); err != nil {
		r.diskErr = err
	} else {
		r.diskUsed, r.diskTotal = du.Used, du.Total
	}
	return r
}
