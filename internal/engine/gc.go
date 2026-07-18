package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/thapakazi/kazi/internal/labels"
	"github.com/thapakazi/kazi/internal/proxy"
	"github.com/thapakazi/kazi/internal/store"
)

// GcItem describes a single reclaimable artifact.
type GcItem struct {
	Kind   string `json:"kind"`   // "stack" | "container" | "allocation"
	Name   string `json:"name"`
	Reason string `json:"reason"` // see reason constants below
}

const (
	gcReasonStopped    = "stopped ephemeral"
	gcReasonTTL        = "ephemeral TTL expired (24h)"
	gcReasonOrphaned   = "orphaned ephemeral container (crash hint)"
	gcReasonAllocation = "allocation for missing stack"
)

// GcPlan selects reclaimable items in spec order:
//  1. ephemeral manifests whose stack is fully stopped, OR older than the
//     config cleanup TTL (spec.cleanup.ephemeralTTL; createdAt absent → treat
//     as expired);
//  2. containers wearing label kazi.ephemeral=true whose kazi.stack has NO
//     manifest;
//  3. port allocations whose stack has neither manifest nor containers.
func (e *Engine) GcPlan(ctx context.Context) ([]GcItem, error) {
	manifests, err := store.ListStacks()
	if err != nil {
		return nil, fmt.Errorf("gc plan: listing stacks: %w", err)
	}

	cs, err := e.RT.Ps(ctx)
	if err != nil {
		return nil, fmt.Errorf("gc plan: listing containers: %w", err)
	}

	// Parse TTL from config.
	ttlStr := e.Cfg.Spec.Cleanup.EphemeralTTL
	if ttlStr == "" {
		ttlStr = "24h"
	}
	ttl, err := time.ParseDuration(ttlStr)
	if err != nil {
		ttl = 24 * time.Hour
	}
	now := time.Now().UTC()

	// Build manifest name set and ephemeral set for fast lookup.
	manifestByName := make(map[string]store.Manifest, len(manifests))
	for _, m := range manifests {
		manifestByName[m.Metadata.Name] = m
	}

	// Build a set of stack names that have ≥1 running container.
	// For compose stacks: kazi.stack or compose project label.
	// For image stacks: kazi.stack label directly.
	runningByStack := make(map[string]int) // stack name → running container count
	for _, c := range cs {
		if c.State != "running" {
			continue
		}
		stackName := c.Labels[labels.Stack]
		if stackName == "" {
			stackName = c.Labels[labels.ComposeProject]
		}
		if stackName == "" {
			continue
		}
		runningByStack[stackName]++
	}

	var items []GcItem

	// --- Phase 1: ephemeral stacks (stopped or TTL-expired) ---
	for _, m := range manifests {
		if !m.Spec.Ephemeral {
			continue
		}
		name := m.Metadata.Name
		running := runningByStack[name]

		// Check TTL expiry first: absent createdAt → expired.
		expired := false
		reason := gcReasonStopped
		if m.Metadata.CreatedAt == "" {
			expired = true
			reason = gcReasonTTL
		} else {
			createdAt, parseErr := time.Parse(time.RFC3339, m.Metadata.CreatedAt)
			if parseErr != nil {
				// Unparseable → treat as expired.
				expired = true
				reason = gcReasonTTL
			} else if now.Sub(createdAt) > ttl {
				expired = true
				reason = gcReasonTTL
			}
		}

		if expired || running == 0 {
			items = append(items, GcItem{Kind: "stack", Name: name, Reason: reason})
		}
	}

	// Build set of stack names already scheduled for gc (to avoid double-picking
	// their containers in phase 2).
	scheduledStacks := make(map[string]bool, len(items))
	for _, it := range items {
		if it.Kind == "stack" {
			scheduledStacks[it.Name] = true
		}
	}

	// --- Phase 2: orphaned ephemeral containers (kazi.ephemeral=true, no manifest) ---
	for _, c := range cs {
		if c.Labels[labels.Ephemeral] != "true" {
			continue
		}
		stackName := c.Labels[labels.Stack]
		if stackName == "" {
			// No stack label → orphan.
			items = append(items, GcItem{Kind: "container", Name: c.Name, Reason: gcReasonOrphaned})
			continue
		}
		// Has a manifest → covered by its stack (phase 1 or not eligible).
		if _, hasManifest := manifestByName[stackName]; hasManifest {
			continue
		}
		// No manifest but has a stack label → orphan.
		items = append(items, GcItem{Kind: "container", Name: c.Name, Reason: gcReasonOrphaned})
	}

	// --- Phase 3: port allocations with neither manifest nor containers ---
	ps, loadErr := proxy.LoadPorts()
	if loadErr != nil {
		return nil, fmt.Errorf("gc plan: loading ports: %w", loadErr)
	}

	// Build set of stack names that have any container (running or stopped).
	stacksWithContainers := make(map[string]bool)
	for _, c := range cs {
		sn := c.Labels[labels.Stack]
		if sn == "" {
			sn = c.Labels[labels.ComposeProject]
		}
		if sn != "" {
			stacksWithContainers[sn] = true
		}
	}

	seenAllocStacks := make(map[string]bool)
	for _, alloc := range ps.Allocations {
		if seenAllocStacks[alloc.Stack] {
			continue
		}
		_, hasManifest := manifestByName[alloc.Stack]
		hasContainers := stacksWithContainers[alloc.Stack]
		if !hasManifest && !hasContainers {
			seenAllocStacks[alloc.Stack] = true
			items = append(items, GcItem{Kind: "allocation", Name: alloc.Stack, Reason: gcReasonAllocation})
		}
	}

	return items, nil
}

// GcRun executes a gc plan:
//   - stack items: Teardown (full ephemeral cleanup via T5's e.Teardown);
//   - container items: `rt.Cmd rm -f <name>` (no volume/image pruning — only
//     labeled containers are provably kazi's);
//   - allocation items: FreeStack.
//
// Continues past per-item errors, returns reclaimed items + joined error.
func (e *Engine) GcRun(ctx context.Context, items []GcItem) ([]GcItem, error) {
	var reclaimed []GcItem
	var errs []error

	for _, item := range items {
		var itemErr error
		switch item.Kind {
		case "stack":
			itemErr = e.Teardown(ctx, item.Name)
		case "container":
			itemErr = runCmd(e.RT.Cmd(ctx, "rm", "-f", item.Name))
		case "allocation":
			ps, loadErr := proxy.LoadPorts()
			if loadErr != nil {
				itemErr = fmt.Errorf("loading ports for %q: %w", item.Name, loadErr)
			} else {
				ps.FreeStack(item.Name)
			}
		default:
			itemErr = fmt.Errorf("unknown gc item kind %q", item.Kind)
		}

		if itemErr != nil {
			errs = append(errs, fmt.Errorf("%s %q: %w", item.Kind, item.Name, itemErr))
		} else {
			reclaimed = append(reclaimed, item)
		}
	}

	return reclaimed, errors.Join(errs...)
}

// GcDebris returns the count of reclaimable items (a nudge for kazi status).
// Returns 0 on any error (best-effort).
func (e *Engine) GcDebris(ctx context.Context) int {
	items, err := e.GcPlan(ctx)
	if err != nil {
		return 0
	}
	return len(items)
}

// runCmd executes a pre-built *exec.Cmd and returns any error.
func runCmd(cmd interface{ Run() error }) error {
	return cmd.Run()
}
