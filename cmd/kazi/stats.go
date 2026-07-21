package main

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/thapakazi/kazi/internal/engine"
)

var statsHost bool

var statsCmd = &cobra.Command{
	Use:   "stats [stack]",
	Short: "Live resource stats for kazi-visible containers, or the host (--host)",
	Long: `A scoped, kazi-grouped snapshot of container resource usage (CPU, memory,
net I/O, block I/O, PIDs). With no stack, every kazi-visible container grouped
by stack; with a stack name, just its services. --host reports host CPU/memory/
disk instead.

The snapshot is one-shot (pipe- and agent-safe); the continuous, animated view
is the TUI's Stats tab. Container stats come only from '<runtime> stats' — never
the Docker API socket.`,
	Args: rangeArgs(0, 1),
	RunE: func(cmd *cobra.Command, args []string) error {
		eng, err := buildEngine()
		if err != nil {
			return err
		}

		if statsHost {
			hs, err := eng.HostStats(cmd.Context())
			if err != nil {
				return err
			}
			if jsonOut {
				return printEnvelope("HostStats", []engine.HostStats{hs})
			}
			return renderHostStats(hs)
		}

		stack := ""
		if len(args) == 1 {
			stack = args[0]
		}
		cs, err := eng.Stats(cmd.Context(), stack)
		if err != nil {
			return err
		}
		if jsonOut {
			return printEnvelope("StatsList", cs)
		}
		return renderContainerStats(cs)
	},
}

// renderContainerStats prints the one-shot snapshot as a stack-grouped table.
func renderContainerStats(cs []engine.ContainerStats) error {
	if len(cs) == 0 {
		fmt.Println("no running containers")
		return nil
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "STACK\tSERVICE\tCPU%\tMEM\tNET (rx/tx)\tBLOCK (r/w)\tPIDS")
	for _, c := range cs {
		stack := c.Stack
		if stack == "" {
			stack = "-"
		}
		svc := c.Service
		if svc == "" {
			svc = c.Name
		}
		fmt.Fprintf(w, "%s\t%s\t%.1f\t%s / %s (%.0f%%)\t%s / %s\t%s / %s\t%d\n",
			stack, svc, c.CPUPercent,
			c.MemUsage, c.MemLimit, c.MemPercent,
			c.NetRx, c.NetTx, c.BlockRead, c.BlockWrite, c.PIDs)
	}
	return w.Flush()
}

// renderHostStats prints the host CPU/memory/disk line. Metrics that degraded to
// zero (unsupported on the platform) are shown as "—".
func renderHostStats(hs engine.HostStats) error {
	cpu := fmt.Sprintf("%.0f%% (%d cores)", hs.CPUPercent, hs.CPUCores)
	mem := "—"
	if hs.MemTotal > 0 {
		mem = fmt.Sprintf("%s / %s (%.0f%%)", humanBytes(hs.MemUsed), humanBytes(hs.MemTotal), pct(hs.MemUsed, hs.MemTotal))
	}
	disk := "—"
	if hs.DiskTotal > 0 {
		disk = fmt.Sprintf("%s / %s (%.0f%%)", humanBytes(hs.DiskUsed), humanBytes(hs.DiskTotal), pct(hs.DiskUsed, hs.DiskTotal))
	}
	w := tabwriter.NewWriter(os.Stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintln(w, "CPU\tMEMORY\tDISK")
	fmt.Fprintf(w, "%s\t%s\t%s\n", cpu, mem, disk)
	return w.Flush()
}

// pct is a used/total percentage, guarding against a zero denominator.
func pct(used, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return float64(used) / float64(total) * 100
}

// humanBytes formats a byte count with a binary-unit suffix (KiB, MiB, …).
func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(b)/float64(div), "KMGTPE"[exp])
}

func init() {
	statsCmd.Flags().BoolVar(&statsHost, "host", false, "report host CPU/memory/disk instead of containers")
	rootCmd.AddCommand(statsCmd)
}
