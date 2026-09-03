// Package config loads and validates kmap's single YAML configuration file.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// DefaultSelector is used when neither the alias nor defaults specify one.
const DefaultSelector = "app={{.Workload}}"

// DefaultNamespace is used when neither the alias nor the environment sets one.
const DefaultNamespace = "default"

type Config struct {
	Version      int                    `yaml:"version"`
	Defaults     Defaults               `yaml:"defaults"`
	Environments map[string]Environment `yaml:"environments"`
	Aliases      map[string]Alias       `yaml:"aliases"`

	Path string `yaml:"-"` // where this was loaded from; for error messages
}

type Defaults struct {
	Environment string   `yaml:"environment"`
	Aliases     []string `yaml:"aliases"`
	Selector    string   `yaml:"selector"`
}

// Environment is reached either by running Command, or by running kubectl with
// Context. Exactly one must be set; validation enforces that.
type Environment struct {
	Command   []string `yaml:"command"`
	Context   string   `yaml:"context"`
	Namespace string   `yaml:"namespace"`
	Protected bool     `yaml:"protected"`
}

// Alias is either a single Mapping used everywhere (All), or one Mapping per
// environment (Envs). Exactly one field is non-nil.
type Alias struct {
	All  *Mapping
	Envs map[string]*Mapping

	Line int // line of the alias key, for error messages
}

// Mapping names a workload, and optionally where and how to find its pods.
type Mapping struct {
	Workload  string   `yaml:"workload"`
	Workloads []string `yaml:"workloads"`
	Namespace string   `yaml:"namespace"`
	Selector  string   `yaml:"selector"`

	Line int `yaml:"-"`
}

// UnmarshalYAML accepts either "name", "name@namespace", or an object.
func (m *Mapping) UnmarshalYAML(n *yaml.Node) error {
	m.Line = n.Line
	if n.Kind == yaml.ScalarNode {
		name, ns, _ := strings.Cut(n.Value, "@")
		m.Workload, m.Namespace = name, ns
		return nil
	}
	type raw Mapping // avoid recursing into this method
	var r raw
	if err := n.Decode(&r); err != nil {
		return err
	}
	r.Line = n.Line
	*m = Mapping(r)
	return nil
}

// UnmarshalYAML accepts either a scalar (one workload for every environment)
// or a map keyed by environment name.
func (a *Alias) UnmarshalYAML(n *yaml.Node) error {
	a.Line = n.Line
	if n.Kind == yaml.ScalarNode {
		var m Mapping
		if err := m.UnmarshalYAML(n); err != nil {
			return err
		}
		a.All = &m
		return nil
	}
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("line %d: alias must be a name or a map of environments", n.Line)
	}
	a.Envs = map[string]*Mapping{}
	return n.Decode(&a.Envs)
}

// Workloads returns every workload named by this mapping, always at least one
// entry unless the mapping is empty.
func (m *Mapping) WorkloadList() []string {
	if len(m.Workloads) > 0 {
		return m.Workloads
	}
	if m.Workload == "" {
		return nil
	}
	return []string{m.Workload}
}

// DefaultPath resolves the config location: $KMAP_CONFIG, else
// $XDG_CONFIG_HOME/kmap/config.yaml, else ~/.config/kmap/config.yaml.
func DefaultPath() string {
	if p := os.Getenv("KMAP_CONFIG"); p != "" {
		return p
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "config.yaml"
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "kmap", "config.yaml")
}

// Load reads and decodes the config, applying defaults. It does not validate;
// callers run Validate separately so they can report every problem at once.
func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err // callers use os.IsNotExist to offer `kmap init`
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	cfg.Path = path
	cfg.applyDefaults()
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if c.Defaults.Selector == "" {
		c.Defaults.Selector = DefaultSelector
	}
	if c.Defaults.Environment == "" && len(c.Environments) == 1 {
		for name := range c.Environments {
			c.Defaults.Environment = name
		}
	}
	if len(c.Defaults.Aliases) == 0 {
		for name := range c.Aliases {
			c.Defaults.Aliases = append(c.Defaults.Aliases, name)
		}
		sort.Strings(c.Defaults.Aliases)
	}
}

// AliasNames returns every configured alias, sorted. This is the single source
// of truth for "which aliases exist" — there is no separate list.
func (c *Config) AliasNames() []string {
	names := make([]string, 0, len(c.Aliases))
	for n := range c.Aliases {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// EnvNames returns every configured environment, sorted.
func (c *Config) EnvNames() []string {
	names := make([]string, 0, len(c.Environments))
	for n := range c.Environments {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
