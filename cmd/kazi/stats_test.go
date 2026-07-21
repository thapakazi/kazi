package main

import "testing"

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   uint64
		want string
	}{
		{0, "0B"},
		{512, "512B"},
		{1024, "1.0KiB"},
		{1536, "1.5KiB"},
		{12 << 30, "12.0GiB"},
		{36 << 30, "36.0GiB"},
	}
	for _, c := range cases {
		if got := humanBytes(c.in); got != c.want {
			t.Errorf("humanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPctGuardsZeroDenominator(t *testing.T) {
	if got := pct(1, 0); got != 0 {
		t.Errorf("pct(1,0) = %v, want 0", got)
	}
	if got := pct(1, 2); got != 50 {
		t.Errorf("pct(1,2) = %v, want 50", got)
	}
}
