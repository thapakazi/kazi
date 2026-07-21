package tui

import (
	"strconv"
	"strings"
	"unicode"
)

// sparkBlocks are the eight vertical block glyphs, low → high; a lightweight,
// dependency-free renderer (no ntcharts). spark maps a numeric series onto them.
var sparkBlocks = []rune("▁▂▃▄▅▆▇█")

// spark renders the last `width` values of vals as block glyphs. max fixes the
// top of the scale (e.g. 100 for a percentage); max <= 0 auto-scales to the
// series' own peak, so a varying series shows its shape. An empty series or
// non-positive width yields "".
func spark(vals []float64, width int, max float64) string {
	if width <= 0 || len(vals) == 0 {
		return ""
	}
	if len(vals) > width {
		vals = vals[len(vals)-width:]
	}
	if max <= 0 {
		for _, v := range vals {
			if v > max {
				max = v
			}
		}
	}
	var b strings.Builder
	for _, v := range vals {
		idx := 0
		if max > 0 {
			idx = int(v/max*float64(len(sparkBlocks)-1) + 0.5)
		}
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		b.WriteRune(sparkBlocks[idx])
	}
	return b.String()
}

// byteUnits maps a lower-cased size suffix to its multiplier. Docker mixes IEC
// (MiB, GiB — memory) and SI (kB, MB — net/block) spellings; both are treated as
// binary (1024) here, which is exact for the memory column the aggregate sums
// and close enough for the SI columns (they aren't aggregated).
var byteUnits = map[string]float64{
	"b": 1,
	"kb": 1 << 10, "kib": 1 << 10,
	"mb": 1 << 20, "mib": 1 << 20,
	"gb": 1 << 30, "gib": 1 << 30,
	"tb": 1 << 40, "tib": 1 << 40,
	"pb": 1 << 50, "pib": 1 << 50,
}

// parseHumanBytes parses one of docker's human byte strings ("128MiB", "1.2GB",
// "0B") into bytes. An unrecognised or empty string is 0, so a bad column never
// skews the aggregate.
func parseHumanBytes(s string) uint64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	i := 0
	for i < len(s) && (unicode.IsDigit(rune(s[i])) || s[i] == '.') {
		i++
	}
	num, err := strconv.ParseFloat(s[:i], 64)
	if err != nil {
		return 0
	}
	unit := strings.ToLower(strings.TrimSpace(s[i:]))
	if unit == "" {
		return uint64(num)
	}
	mult, ok := byteUnits[unit]
	if !ok {
		return 0
	}
	return uint64(num * mult)
}
