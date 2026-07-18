package compose

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

type ServiceInfo struct {
	Name      string
	Ports     []int
	Published map[int]int
	Networks  []string
}

type rawConfig struct {
	Services map[string]rawService `json:"services"`
}

type rawService struct {
	Ports []struct {
		Target    int    `json:"target"`
		Published string `json:"published"`
	} `json:"ports"`
	Expose   []any          `json:"expose"`
	Networks map[string]any `json:"networks"`
}

// ParseConfig extracts the per-service port/network view from
// `<runtime> compose config --format json` output.
func ParseConfig(jsonBytes []byte) ([]ServiceInfo, error) {
	var raw rawConfig
	if err := json.Unmarshal(jsonBytes, &raw); err != nil {
		return nil, fmt.Errorf("parsing compose config JSON: %w", err)
	}
	var out []ServiceInfo
	for name, rs := range raw.Services {
		si := ServiceInfo{Name: name, Published: map[int]int{}}
		seen := map[int]bool{}
		for _, p := range rs.Ports {
			if p.Target == 0 {
				continue
			}
			if !seen[p.Target] {
				seen[p.Target] = true
				si.Ports = append(si.Ports, p.Target)
			}
			if hp, err := strconv.Atoi(p.Published); err == nil && hp > 0 {
				si.Published[p.Target] = hp
			}
		}
		for _, e := range rs.Expose {
			var port int
			switch v := e.(type) {
			case float64:
				port = int(v)
			case string:
				port, _ = strconv.Atoi(v)
			}
			if port > 0 && !seen[port] {
				seen[port] = true
				si.Ports = append(si.Ports, port)
			}
		}
		sort.Ints(si.Ports)
		for n := range rs.Networks {
			si.Networks = append(si.Networks, n)
		}
		sort.Strings(si.Networks)
		out = append(out, si)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
