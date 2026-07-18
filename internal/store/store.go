// Package store reads and writes kazi's YAML manifests under the config
// root (~/.config/kazi, overridable via KAZI_CONFIG_DIR).
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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
	Name      string `yaml:"name"`
	CreatedAt string `yaml:"createdAt,omitempty"` // RFC3339, set at registration
}

type Spec struct {
	Source    Source            `yaml:"source"`
	Proxy     *ProxySpec        `yaml:"proxy,omitempty"`
	Expose    []ExposeSpec      `yaml:"expose,omitempty"`
	System    bool              `yaml:"system,omitempty"`    // system stacks (kazi-proxy): protected from rm
	Ephemeral bool              `yaml:"ephemeral,omitempty"` // gc reclaims when true
	Values    map[string]string `yaml:"values,omitempty"`    // --set overrides / run -e env
	Volumes   []string          `yaml:"volumes,omitempty"`   // run -v entries ("vol:/path")
}

// Source is a union: exactly one arm set.
type Source struct {
	Compose    string   `yaml:"compose,omitempty"`
	Image      string   `yaml:"image,omitempty"`      // ad-hoc: kazi run
	Template   string   `yaml:"template,omitempty"`   // try-stacks: catalog template name
	Containers []string `yaml:"containers,omitempty"` // adopted container names
}

// Kind returns "compose"|"image"|"template"|"containers"|"" based on which arm is set.
func (s Source) Kind() string {
	switch {
	case s.Compose != "":
		return "compose"
	case s.Image != "":
		return "image"
	case s.Template != "":
		return "template"
	case len(s.Containers) > 0:
		return "containers"
	default:
		return ""
	}
}

// ProxySpec configures the reverse-proxy routing for a stack.
type ProxySpec struct {
	Service  string `yaml:"service,omitempty"`   // primary HTTP service
	HTTPPort int    `yaml:"http_port,omitempty"` // its container port
	Enabled  *bool  `yaml:"enabled,omitempty"`   // nil or true = routing on
}

// ExposeSpec maps a single service port for external access.
type ExposeSpec struct {
	Service string `yaml:"service"`
	Port    string `yaml:"port"` // "auto" or a fixed number as string
}

type Config struct {
	APIVersion string     `yaml:"apiVersion"`
	Kind       string     `yaml:"kind"`
	Spec       ConfigSpec `yaml:"spec"`
}

// CleanupConfig controls gc behaviour.
type CleanupConfig struct {
	EphemeralTTL string `yaml:"ephemeralTTL,omitempty"` // default "24h"
}

type ConfigSpec struct {
	Runtime string        `yaml:"runtime"`           // auto | docker | podman | nerdctl
	Proxy   ProxyConfig   `yaml:"proxy,omitempty"`   // port-forwarding allowlists
	Ports   PortsConfig   `yaml:"ports,omitempty"`   // ephemeral port range
	Cleanup CleanupConfig `yaml:"cleanup,omitempty"` // gc policy
}

// ProxyConfig lists which well-known ports the proxy should forward.
type ProxyConfig struct {
	TCPPorts  []int `yaml:"tcpPorts,omitempty"`
	HTTPPorts []int `yaml:"httpPorts,omitempty"`
}

// PortsConfig controls the ephemeral port range allocated to stacks.
type PortsConfig struct {
	Range string `yaml:"range,omitempty"` // "42000-42999"
}

// Default port lists and range — seeded into any config that omits them.
var DefaultTCPPorts = []int{1521, 3306, 5432, 5672, 6379, 9092, 11211, 27017}
var DefaultHTTPPorts = []int{80, 81, 3000, 3001, 4200, 5000, 5173, 8000, 8080, 8081, 8888, 9000}

const DefaultPortRange = "42000-42999"

var dnsLabelRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// IsDNSLabel reports whether name can be used as a *.localhost hostname
// component (stack names become hostnames in M1).
func IsDNSLabel(name string) bool {
	return len(name) <= 63 && dnsLabelRe.MatchString(name)
}

// applyDefaults seeds any empty config fields with their default values.
func applyDefaults(c *Config) {
	if c.Spec.Runtime == "" {
		c.Spec.Runtime = "auto"
	}
	// Copy the default slices so callers appending to the loaded config
	// can never mutate the shared package-level defaults.
	if len(c.Spec.Proxy.TCPPorts) == 0 {
		c.Spec.Proxy.TCPPorts = append([]int(nil), DefaultTCPPorts...)
	}
	if len(c.Spec.Proxy.HTTPPorts) == 0 {
		c.Spec.Proxy.HTTPPorts = append([]int(nil), DefaultHTTPPorts...)
	}
	if c.Spec.Ports.Range == "" {
		c.Spec.Ports.Range = DefaultPortRange
	}
	if c.Spec.Cleanup.EphemeralTTL == "" {
		c.Spec.Cleanup.EphemeralTTL = "24h"
	}
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

// validName rejects empty names and names that could escape the stacks
// directory via path traversal.
func validName(name string) error {
	if name == "" || name == "." || name == ".." ||
		strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("invalid stack name %q", name)
	}
	return nil
}

func SaveStack(m Manifest) error {
	if err := validName(m.Metadata.Name); err != nil {
		return err
	}
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
	if err := validName(name); err != nil {
		return Manifest{}, err
	}
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
	if err := validName(name); err != nil {
		return err
	}
	err := os.Remove(stackPath(name))
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return err
}

// LoadConfig returns defaults when config.yaml is absent; missing fields are
// seeded with defaults even when the file is partially populated.
func LoadConfig() (Config, error) {
	b, err := os.ReadFile(filepath.Join(Root(), "config.yaml"))
	if errors.Is(err, os.ErrNotExist) {
		c := Config{APIVersion: "kazi.dev/v1alpha1", Kind: "Config"}
		applyDefaults(&c)
		return c, nil
	}
	if err != nil {
		return Config{}, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("parsing config.yaml: %w", err)
	}
	applyDefaults(&c)
	return c, nil
}
