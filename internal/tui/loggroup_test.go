package tui

import (
	"strings"
	"testing"
)

func TestNormalizeLogLine(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"blog-web-1  | GET /x/12 200", "blog-web-#  | GET /x/# #"},
		{"GET /x/97 200", "GET /x/# #"},
		{"req 550e8400-e29b-41d4-a716-446655440000 done", "req # done"},
		{"2026-07-18T12:01:04Z ERROR could not connect", "# ERROR could not connect"},
		{"12:01:04 ready to accept connections", "# ready to accept connections"},
		{"commit deadbeefcafe pushed", "commit # pushed"},
	}
	for _, c := range cases {
		if got := normalizeLogLine(c.in); got != c.want {
			t.Errorf("normalizeLogLine(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestGroupLogsCollapsesAndOrders(t *testing.T) {
	lines := []string{
		"GET /x/12 200",
		"POST /y 500",
		"GET /x/97 200",
		"GET /x/3 200",
	}
	groups := groupLogs(lines)
	if len(groups) != 2 {
		t.Fatalf("groupLogs buckets = %d, want 2 (%+v)", len(groups), groups)
	}
	// Most-frequent first: the three GET lines collapse into one bucket.
	if groups[0].count != 3 {
		t.Fatalf("top bucket count = %d, want 3", groups[0].count)
	}
	if !strings.Contains(groups[0].pattern, "GET /x/# #") {
		t.Fatalf("top pattern = %q, want templated GET line", groups[0].pattern)
	}
	if groups[1].count != 1 {
		t.Fatalf("second bucket count = %d, want 1", groups[1].count)
	}
}

func TestGroupedLogLinesRender(t *testing.T) {
	out := groupedLogLines([]string{"GET /x/1 200", "GET /x/2 200"})
	if len(out) != 1 {
		t.Fatalf("grouped lines = %d, want 1", len(out))
	}
	if !strings.Contains(out[0], "2 × ") {
		t.Fatalf("grouped line = %q, want a count × pattern form", out[0])
	}
}
