package tui

import "testing"

func TestSparkFixedScale(t *testing.T) {
	// 0/50/100 against a fixed 0..100 scale → low, mid, top glyphs.
	if got := spark([]float64{0, 50, 100}, 8, 100); got != "▁▅█" {
		t.Errorf("spark fixed = %q, want ▁▅█", got)
	}
	// A steady low value stays at the floor glyph (does not saturate).
	if got := spark([]float64{5, 5, 5}, 8, 100); got != "▁▁▁" {
		t.Errorf("spark steady = %q, want ▁▁▁", got)
	}
}

func TestSparkAutoScaleAndClamps(t *testing.T) {
	// Auto-scale (max<=0) normalizes to the series peak, showing its shape.
	if got := spark([]float64{1, 2, 4}, 8, 0); got != "▃▅█" {
		t.Errorf("spark auto = %q, want ▃▅█", got)
	}
	if got := spark(nil, 8, 0); got != "" {
		t.Errorf("empty series = %q, want empty", got)
	}
	// Only the last `width` samples are drawn.
	if got := spark([]float64{9, 9, 9, 1}, 2, 0); len([]rune(got)) != 2 {
		t.Errorf("width clamp = %q, want 2 glyphs", got)
	}
}

func TestParseHumanBytes(t *testing.T) {
	cases := []struct {
		in   string
		want uint64
	}{
		{"128MiB", 128 << 20},
		{"512", 512},
		{"0B", 0},
		{"", 0},
		{"6.7GB", 7194070220}, // uint64(6.7 * 2^30), truncated
		{"1.5KiB", 1536},
		{"bogus", 0},
	}
	for _, c := range cases {
		if got := parseHumanBytes(c.in); got != c.want {
			t.Errorf("parseHumanBytes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFmtBytes(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{512, "512B"},
		{1024, "1.0K"},
		{36 << 30, "36.0G"},
		{12 << 30, "12.0G"},
	}
	for _, c := range cases {
		if got := fmtBytes(c.in); got != c.want {
			t.Errorf("fmtBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}
