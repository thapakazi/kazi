package engine

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"github.com/thapakazi/kazi/internal/compose"
)

// ContainerEnv is one container's runtime environment, for the Env tab. Env is a
// container's `.Config.Env` (fixed at creation), so callers may cache it for the
// container's lifetime. Err is set (and Env nil) when that one container's
// inspect failed, so a single bad container doesn't sink the whole stack view.
type ContainerEnv struct {
	Service string   `json:"service"`       // compose service (or container name if none)
	Name    string   `json:"name"`          // container name
	Env     []string `json:"env,omitempty"` // "KEY=value", sorted by key
	Err     string   `json:"err,omitempty"`
}

// StackEnv returns the environment of every container in a stack, one entry per
// container, sorted by service name. It shells out to `<runtime> inspect
// --format {{json .Config.Env}}` per container (subprocess only — never the API
// socket), matching kazi's runtime-agnostic contract.
func (e *Engine) StackEnv(ctx context.Context, name string) ([]ContainerEnv, error) {
	st, err := e.Status(ctx, name)
	if err != nil {
		return nil, err
	}
	out := make([]ContainerEnv, 0, len(st.Containers))
	for _, c := range st.Containers {
		svc := c.Service
		if svc == "" {
			svc = c.Name
		}
		ce := ContainerEnv{Service: svc, Name: c.Name}
		ref := c.ID
		if ref == "" {
			ref = c.Name
		}
		raw, ierr := compose.Output(e.RT.Cmd(ctx, "inspect", "--format", "{{json .Config.Env}}", ref))
		if ierr != nil {
			ce.Err = ierr.Error()
			out = append(out, ce)
			continue
		}
		var env []string
		// A container with no env inspects as JSON null → env stays nil.
		if s := strings.TrimSpace(raw); s != "" && s != "null" {
			if uerr := json.Unmarshal([]byte(s), &env); uerr != nil {
				ce.Err = uerr.Error()
				out = append(out, ce)
				continue
			}
		}
		sort.Strings(env)
		ce.Env = env
		out = append(out, ce)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Service < out[j].Service })
	return out, nil
}
