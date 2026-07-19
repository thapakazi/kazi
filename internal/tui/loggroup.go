package tui

import (
	"fmt"
	"regexp"
	"sort"
)

// Log-line normalisation for pattern grouping. The order of substitution
// matters: composite tokens (ISO timestamps, UUIDs, hex runs) are templated
// before bare digits, so their internal digits aren't collapsed first and the
// surrounding structure survives as a single `#`.
var (
	reISOTimestamp = regexp.MustCompile(`\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?`)
	reClockTime    = regexp.MustCompile(`\b\d{1,2}:\d{2}:\d{2}(?:\.\d+)?\b`)
	reUUID         = regexp.MustCompile(`\b[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\b`)
	reHexAddr      = regexp.MustCompile(`\b0x[0-9a-fA-F]+\b`)
	reHexRun       = regexp.MustCompile(`\b[0-9a-fA-F]{7,}\b`) // sha/commit/id runs
	reDigits       = regexp.MustCompile(`\d+`)
)

// normalizeLogLine templates the variable parts of a log line — timestamps,
// UUIDs, hex runs, and any remaining digit runs — down to `#`, so that lines
// that differ only in those values collapse to one pattern. It is pure.
func normalizeLogLine(s string) string {
	s = reISOTimestamp.ReplaceAllString(s, "#")
	s = reClockTime.ReplaceAllString(s, "#")
	s = reUUID.ReplaceAllString(s, "#")
	s = reHexAddr.ReplaceAllString(s, "#")
	s = reHexRun.ReplaceAllString(s, "#")
	s = reDigits.ReplaceAllString(s, "#")
	return s
}

// logGroup is one bucket of identically-templated lines.
type logGroup struct {
	count   int
	pattern string
}

// groupLogs buckets lines by their normalised pattern and returns the buckets
// ordered by descending count (ties broken by pattern for stable output). Pure.
func groupLogs(lines []string) []logGroup {
	counts := map[string]int{}
	var order []string
	for _, ln := range lines {
		p := normalizeLogLine(ln)
		if _, seen := counts[p]; !seen {
			order = append(order, p)
		}
		counts[p]++
	}
	groups := make([]logGroup, 0, len(order))
	for _, p := range order {
		groups = append(groups, logGroup{count: counts[p], pattern: p})
	}
	sort.SliceStable(groups, func(i, j int) bool {
		if groups[i].count != groups[j].count {
			return groups[i].count > groups[j].count
		}
		return groups[i].pattern < groups[j].pattern
	})
	return groups
}

// groupedLogLines renders the grouped view as `count × pattern` lines, most
// frequent first — the same shape the viewport draws and copy yanks.
func groupedLogLines(lines []string) []string {
	groups := groupLogs(lines)
	out := make([]string, len(groups))
	for i, g := range groups {
		out[i] = fmt.Sprintf("%5d × %s", g.count, g.pattern)
	}
	return out
}
