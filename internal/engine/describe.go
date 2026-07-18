package engine

import (
	"context"

	"github.com/thapakazi/kazi/internal/store"
)

// StackDetail is the full picture of one stack: live status plus the
// registered manifest's declarations and every reachable endpoint.
type StackDetail struct {
	StackInfo
	Source    string             `json:"source,omitempty"` // compose file path (registered stacks)
	Proxy     *store.ProxySpec   `json:"proxy,omitempty"`
	Expose    []store.ExposeSpec `json:"expose,omitempty"`
	System    bool               `json:"system,omitempty"`
	Endpoints []Endpoint         `json:"endpoints,omitempty"`
}

// Describe returns detailed information for one stack. Manifest fields are
// populated for registered stacks only; endpoints are best-effort (a proxy
// or config failure leaves them empty rather than failing the describe).
func (e *Engine) Describe(ctx context.Context, name string) (StackDetail, error) {
	st, err := e.Status(ctx, name)
	if err != nil {
		return StackDetail{}, err
	}
	d := StackDetail{StackInfo: st}
	if m, err := store.LoadStack(name); err == nil {
		d.Source = m.Spec.Source.Compose
		d.Proxy = m.Spec.Proxy
		d.Expose = m.Spec.Expose
		d.System = m.Spec.System
	}
	if eps, err := e.Urls(ctx, name); err == nil {
		d.Endpoints = eps
	}
	return d, nil
}
