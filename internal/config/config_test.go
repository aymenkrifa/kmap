package config

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadAliasShapes(t *testing.T) {
	p := write(t, `
version: 1
defaults:
  environment: local
  aliases: [api]
environments:
  local:
    command: [klocal]
  prod:
    context: Production
    protected: true
aliases:
  queue: queue
  api:
    local: api-server
    prod: api-gateway@backend
  bundle:
    local:
      workloads: [api, worker]
      selector: "role in (api,worker)"
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// scalar shape applies to every environment
	if got := cfg.Aliases["queue"].All; got == nil || got.Workload != "queue" {
		t.Fatalf("queue: got %+v, want All.Workload=queue", got)
	}

	// per-environment string shape
	if got := cfg.Aliases["api"].Envs["local"].Workload; got != "api-server" {
		t.Errorf("api/local workload = %q, want api-server", got)
	}

	// workload@namespace splits
	prod := cfg.Aliases["api"].Envs["prod"]
	if prod.Workload != "api-gateway" || prod.Namespace != "backend" {
		t.Errorf("api/prod = %+v, want api-gateway in backend", prod)
	}

	// object shape
	b := cfg.Aliases["bundle"].Envs["local"]
	if len(b.Workloads) != 2 || b.Selector != "role in (api,worker)" {
		t.Errorf("bundle/local = %+v", b)
	}

	// environment shapes
	if cfg.Environments["local"].Command[0] != "klocal" {
		t.Errorf("local.command = %v", cfg.Environments["local"].Command)
	}
	if !cfg.Environments["prod"].Protected {
		t.Error("prod should be protected")
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	p := write(t, `
version: 1
environments:
  local: {command: [kubectl]}
aliases:
  queue: queue
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Defaults.Selector != "app={{.Workload}}" {
		t.Errorf("selector default = %q", cfg.Defaults.Selector)
	}
	if cfg.Defaults.Environment != "local" {
		t.Errorf("environment default = %q, want the sole environment", cfg.Defaults.Environment)
	}
	if len(cfg.Defaults.Aliases) != 1 || cfg.Defaults.Aliases[0] != "queue" {
		t.Errorf("aliases default = %v, want all aliases", cfg.Defaults.Aliases)
	}
}

func TestLoadMissingFile(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "nope.yaml"))
	if !os.IsNotExist(err) {
		t.Fatalf("want fs.ErrNotExist, got %v", err)
	}
}

func TestWorkloadList(t *testing.T) {
	tests := []struct {
		name string
		m    *Mapping
		want []string
	}{
		{
			name: "single workload",
			m: &Mapping{
				Workload: "api",
			},
			want: []string{"api"},
		},
		{
			name: "multiple workloads",
			m: &Mapping{
				Workloads: []string{"api", "worker", "cache"},
			},
			want: []string{"api", "worker", "cache"},
		},
		{
			name: "empty mapping",
			m:    &Mapping{},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.m.WorkloadList()
			if len(got) != len(tt.want) {
				t.Errorf("WorkloadList() = %v, want %v", got, tt.want)
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Errorf("WorkloadList()[%d] = %q, want %q", i, v, tt.want[i])
				}
			}
		})
	}
}

func TestAliasNames(t *testing.T) {
	p := write(t, `
version: 1
environments:
  local: {command: [kubectl]}
aliases:
  zebra: zebra
  apple: apple
  mango: mango
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := cfg.AliasNames()
	want := []string{"apple", "mango", "zebra"}

	if len(got) != len(want) {
		t.Errorf("AliasNames() returned %d names, want %d", len(got), len(want))
	}
	for i, v := range got {
		if v != want[i] {
			t.Errorf("AliasNames()[%d] = %q, want %q", i, v, want[i])
		}
	}
}

func TestEnvNames(t *testing.T) {
	p := write(t, `
version: 1
environments:
  staging:
    command: [kubectl]
  prod:
    context: Production
  dev:
    command: [klocal]
aliases:
  queue: queue
`)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	got := cfg.EnvNames()
	want := []string{"dev", "prod", "staging"}

	if len(got) != len(want) {
		t.Errorf("EnvNames() returned %d names, want %d", len(got), len(want))
	}
	for i, v := range got {
		if v != want[i] {
			t.Errorf("EnvNames()[%d] = %q, want %q", i, v, want[i])
		}
	}
}

func TestDefaultPath(t *testing.T) {
	tests := []struct {
		name         string
		kmapConfig   string
		xdgHome      string
		wantContains string
	}{
		{
			name:         "KMAP_CONFIG set",
			kmapConfig:   "/etc/kmap.yaml",
			wantContains: "/etc/kmap.yaml",
		},
		{
			name:         "XDG_CONFIG_HOME set",
			xdgHome:      "/custom/config",
			wantContains: "/custom/config/kmap/config.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.kmapConfig != "" {
				t.Setenv("KMAP_CONFIG", tt.kmapConfig)
			}
			if tt.xdgHome != "" {
				t.Setenv("XDG_CONFIG_HOME", tt.xdgHome)
			}
			got := DefaultPath()
			if got != tt.wantContains {
				t.Errorf("DefaultPath() = %q, want %q", got, tt.wantContains)
			}
		})
	}
}
