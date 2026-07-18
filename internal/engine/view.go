package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/thapakazi/kazi/internal/labels"
	"github.com/thapakazi/kazi/internal/runtime"
	"github.com/thapakazi/kazi/internal/store"
)

type projectGroup struct {
	project string
	dir     string
	cs      []runtime.Container
}

// snapshot runs one ps, groups containers by compose project, matches
// registered manifests (by kazi-<name> project or by working dir), and
// returns stacks plus loose (unmanaged) containers. Discovery is passive:
// one ps per command, no sockets, no polling.
func (e *Engine) snapshot(ctx context.Context) ([]StackInfo, []ContainerInfo, error) {
	manifests, err := store.ListStacks()
	if err != nil {
		return nil, nil, err
	}
	cs, err := e.RT.Ps(ctx)
	if err != nil {
		return nil, nil, err
	}

	byProject := map[string]*projectGroup{}
	var order []string
	var loose []ContainerInfo
	// byName indexes every container so adopted stacks can claim by name.
	byName := map[string]runtime.Container{}
	// kaziStackClaimed tracks compose-project-less containers already claimed
	// by a kazi.stack label so they never fall through to loose.
	kaziStackClaimed := map[string]bool{}
	for _, c := range cs {
		byName[c.Name] = c
		p := c.Labels[labels.ComposeProject]
		if p == "" {
			loose = append(loose, toInfo(c, "", KindUnmanaged))
			continue
		}
		g := byProject[p]
		if g == nil {
			g = &projectGroup{project: p, dir: c.Labels[labels.ComposeWorkingDir]}
			byProject[p] = g
			order = append(order, p)
		}
		g.cs = append(g.cs, c)
	}

	// looseByStack: compose-project-less containers grouped by their kazi.stack
	// label (image stacks launched via `run`).
	looseByStack := map[string][]runtime.Container{}
	for _, c := range cs {
		if c.Labels[labels.ComposeProject] != "" {
			continue
		}
		if s := c.Labels[labels.Stack]; s != "" {
			looseByStack[s] = append(looseByStack[s], c)
		}
	}

	claimed := map[string]bool{}
	var stacks []StackInfo
	for _, m := range manifests {
		srcKind := m.Spec.Source.Kind()

		// Non-compose registered stacks claim their containers directly.
		switch srcKind {
		case "image":
			si := StackInfo{Name: m.Metadata.Name, Kind: KindRegistered, Project: "kazi-" + m.Metadata.Name}
			if grp, ok := looseByStack[m.Metadata.Name]; ok {
				si.Containers = imageInfos(grp, m.Metadata.Name)
				for _, c := range grp {
					kaziStackClaimed[c.Name] = true
				}
			}
			si.Running, si.Total = tally(si.Containers)
			stacks = append(stacks, si)
			continue
		case "containers":
			si := StackInfo{Name: m.Metadata.Name, Kind: KindRegistered}
			for _, name := range m.Spec.Source.Containers {
				if c, ok := byName[name]; ok {
					si.Containers = append(si.Containers, toInfo(c, m.Metadata.Name, KindRegistered))
					kaziStackClaimed[c.Name] = true
				}
			}
			si.Running, si.Total = tally(si.Containers)
			stacks = append(stacks, si)
			continue
		}

		// compose / template registered stacks: match by project or working dir.
		dir := ""
		if m.Spec.Source.Compose != "" {
			dir = filepath.Dir(m.Spec.Source.Compose)
		}
		si := StackInfo{Name: m.Metadata.Name, Kind: KindRegistered, Dir: dir, Project: "kazi-" + m.Metadata.Name}
		for _, p := range order {
			g := byProject[p]
			if claimed[p] {
				continue
			}
			if p == "kazi-"+m.Metadata.Name || (dir != "" && g.dir == dir) {
				si.Project = p
				si.Containers = toInfos(g.cs, m.Metadata.Name, KindRegistered)
				claimed[p] = true
				break
			}
		}
		si.Running, si.Total = tally(si.Containers)
		stacks = append(stacks, si)
	}

	// Adopted/image containers claimed above must not appear as loose. Rebuild
	// the loose list dropping anything a manifest claimed by name.
	if len(kaziStackClaimed) > 0 {
		filtered := loose[:0]
		for _, ci := range loose {
			if kaziStackClaimed[ci.Name] {
				continue
			}
			filtered = append(filtered, ci)
		}
		loose = filtered
	}
	for _, p := range order {
		if claimed[p] {
			continue
		}
		g := byProject[p]
		si := StackInfo{Name: p, Kind: KindDiscovered, Dir: g.dir, Project: p,
			Containers: toInfos(g.cs, p, KindDiscovered)}
		si.Running, si.Total = tally(si.Containers)
		stacks = append(stacks, si)
	}
	// Sort by priority rank (registered=0, discovered=1, unmanaged=2) then
	// by name within each tier. Alphabetical Kind ordering is wrong:
	// "discovered" < "registered" < "unmanaged" would put discovered first.
	kindRank := map[Kind]int{KindRegistered: 0, KindDiscovered: 1, KindUnmanaged: 2}
	sort.Slice(stacks, func(i, j int) bool {
		ri, rj := kindRank[stacks[i].Kind], kindRank[stacks[j].Kind]
		if ri != rj {
			return ri < rj
		}
		return stacks[i].Name < stacks[j].Name
	})
	return stacks, loose, nil
}

func toInfo(c runtime.Container, stack string, kind Kind) ContainerInfo {
	return ContainerInfo{
		ID: c.ID, Name: c.Name, Image: c.Image,
		Service: c.Labels[labels.ComposeService],
		State:   c.State, Status: c.Status, Health: healthOf(c.Status),
		Ports: c.Ports, Stack: stack, Kind: kind,
	}
}

func toInfos(cs []runtime.Container, stack string, kind Kind) []ContainerInfo {
	out := make([]ContainerInfo, 0, len(cs))
	for _, c := range cs {
		out = append(out, toInfo(c, stack, kind))
	}
	return out
}

// imageInfos annotates an image stack's containers: Service is the stack name
// (image stacks have a single service named after the stack, not a compose
// service label).
func imageInfos(cs []runtime.Container, stack string) []ContainerInfo {
	out := make([]ContainerInfo, 0, len(cs))
	for _, c := range cs {
		info := toInfo(c, stack, KindRegistered)
		info.Service = stack
		out = append(out, info)
	}
	return out
}

func tally(cs []ContainerInfo) (running, total int) {
	for _, c := range cs {
		if c.State == "running" {
			running++
		}
	}
	return running, len(cs)
}

// List returns all registered + discovered stacks (unmanaged loose
// containers appear only in Ps).
func (e *Engine) List(ctx context.Context) ([]StackInfo, error) {
	stacks, _, err := e.snapshot(ctx)
	return stacks, err
}

// Ps returns every container on the runtime, annotated with stack + kind.
func (e *Engine) Ps(ctx context.Context) ([]ContainerInfo, error) {
	stacks, loose, err := e.snapshot(ctx)
	if err != nil {
		return nil, err
	}
	var out []ContainerInfo
	for _, s := range stacks {
		out = append(out, s.Containers...)
	}
	out = append(out, loose...)
	return out, nil
}

// Status returns per-service detail for one stack.
// For registered stacks, it verifies the manifest's compose file still exists
// on disk and returns an actionable error if it does not.
func (e *Engine) Status(ctx context.Context, name string) (StackInfo, error) {
	stacks, _, err := e.snapshot(ctx)
	if err != nil {
		return StackInfo{}, err
	}
	for _, s := range stacks {
		if s.Name == name {
			if s.Kind == KindRegistered {
				m, loadErr := store.LoadStack(name)
				if loadErr == nil && m.Spec.Source.Kind() == "compose" {
					composePath := m.Spec.Source.Compose
					if _, statErr := os.Stat(composePath); statErr != nil {
						return StackInfo{}, fmt.Errorf("stack %q manifest points at %s, which no longer exists; fix the path or `kazi rm %s`",
							name, composePath, name)
					}
				}
			}
			return s, nil
		}
	}
	return StackInfo{}, fmt.Errorf("%w: %s", ErrStackNotFound, name)
}
