package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// StaticRoute maps a hostname to a host port: <host>.localhost is reverse-proxied
// to host.docker.internal:<port>. It exposes services kazi doesn't manage
// (externally-run compose stacks, or any local port) with a pretty TLS URL.
type StaticRoute struct {
	Host  string `yaml:"host"`            // *.localhost subdomain
	Port  int    `yaml:"port"`            // published host port to proxy to
	Stack string `yaml:"stack,omitempty"` // originating stack (kazi route from), for urls grouping
	Note  string `yaml:"note,omitempty"`
}

// RoutesFile is the on-disk shape of routes.yaml.
type RoutesFile struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Spec       RoutesSpec `yaml:"spec"`
}

type RoutesSpec struct {
	Routes []StaticRoute `yaml:"routes,omitempty"`
}

func routesPath() string { return filepath.Join(Root(), "routes.yaml") }

// LoadRoutes reads the static routes, returning an empty slice when absent.
func LoadRoutes() ([]StaticRoute, error) {
	b, err := os.ReadFile(routesPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var f RoutesFile
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("parsing routes.yaml: %w", err)
	}
	return f.Spec.Routes, nil
}

// SaveRoutes writes the static routes to routes.yaml.
func SaveRoutes(routes []StaticRoute) error {
	if err := os.MkdirAll(Root(), 0o755); err != nil {
		return err
	}
	f := RoutesFile{APIVersion: "kazi.dev/v1alpha1", Kind: "Routes", Spec: RoutesSpec{Routes: routes}}
	b, err := yaml.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(routesPath(), b, 0o644)
}
