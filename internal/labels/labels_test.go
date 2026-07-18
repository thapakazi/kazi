package labels

import "testing"

func TestParseDockerCSV(t *testing.T) {
	m := ParseDockerCSV("com.docker.compose.project=blog,kazi.managed=true,empty=")
	if m["com.docker.compose.project"] != "blog" {
		t.Errorf("project = %q", m["com.docker.compose.project"])
	}
	if m["kazi.managed"] != "true" {
		t.Errorf("managed = %q", m["kazi.managed"])
	}
	if v, ok := m["empty"]; !ok || v != "" {
		t.Errorf("empty = %q ok=%v", v, ok)
	}
	if len(ParseDockerCSV("")) != 0 {
		t.Error("empty input should give empty map")
	}
}
