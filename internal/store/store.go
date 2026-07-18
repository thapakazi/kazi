// Package store reads and writes kazi's YAML manifests under the config
// root (~/.config/kazi, overridable via KAZI_CONFIG_DIR).
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

var ErrNotFound = errors.New("stack manifest not found")

type Manifest struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`
}

type Metadata struct {
	Name string `yaml:"name"`
}

type Spec struct {
	Source Source `yaml:"source"`
}

// Source is a union: exactly one arm set. compose is the only arm
// implemented in M0; image and template are reserved for M2 — do not add
// fields here without a spec update.
type Source struct {
	Compose string `yaml:"compose,omitempty"`
}

type Config struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Spec       ConfigSpec `yaml:"spec"`
}

type ConfigSpec struct {
	Runtime string `yaml:"runtime"` // auto | docker | podman | nerdctl
}

func Root() string {
	if d := os.Getenv("KAZI_CONFIG_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".kazi"
	}
	return filepath.Join(home, ".config", "kazi")
}

func stackPath(name string) string {
	return filepath.Join(Root(), "stacks", name+".yaml")
}

func SaveStack(m Manifest) error {
	if err := os.MkdirAll(filepath.Join(Root(), "stacks"), 0o755); err != nil {
		return err
	}
	b, err := yaml.Marshal(m)
	if err != nil {
		return err
	}
	return os.WriteFile(stackPath(m.Metadata.Name), b, 0o644)
}

func LoadStack(name string) (Manifest, error) {
	b, err := os.ReadFile(stackPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return Manifest{}, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := yaml.Unmarshal(b, &m); err != nil {
		return Manifest{}, fmt.Errorf("parsing %s: %w", stackPath(name), err)
	}
	return m, nil
}

func ListStacks() ([]Manifest, error) {
	entries, err := os.ReadDir(filepath.Join(Root(), "stacks"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []Manifest
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		m, err := LoadStack(strings.TrimSuffix(e.Name(), ".yaml"))
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}

func DeleteStack(name string) error {
	err := os.Remove(stackPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return err
}

// LoadConfig returns defaults (runtime auto) when config.yaml is absent.
func LoadConfig() (Config, error) {
	def := Config{APIVersion: "kazi.dev/v1alpha1", Kind: "Config", Spec: ConfigSpec{Runtime: "auto"}}
	b, err := os.ReadFile(filepath.Join(Root(), "config.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		return def, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("parsing config.yaml: %w", err)
	}
	if c.Spec.Runtime == "" {
		c.Spec.Runtime = "auto"
	}
	return c, nil
}
