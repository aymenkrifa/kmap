package cmd

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/aymenkrifa/kmap/internal/config"
)

func testConfig(t *testing.T) *config.Config {
	t.Helper()
	body := `
version: 1
defaults: {environment: local, aliases: [api]}
environments:
  local: {command: [klocal]}
  prod:  {context: Production}
aliases:
  api: {local: api-server, prod: api-gateway@backend}
  worker: worker
`
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestSplitEnvConsumesLeadingEnvironment(t *testing.T) {
	cfg := testConfig(t)
	env, rest := splitEnv(cfg, []string{"prod", "api"})
	if env != "prod" || !reflect.DeepEqual(rest, []string{"api"}) {
		t.Errorf("got %q %v", env, rest)
	}
}

func TestSplitEnvFallsBackToDefault(t *testing.T) {
	cfg := testConfig(t)
	env, rest := splitEnv(cfg, []string{"api"})
	if env != "local" || !reflect.DeepEqual(rest, []string{"api"}) {
		t.Errorf("got %q %v", env, rest)
	}
}

func TestSplitEnvWithNoArgs(t *testing.T) {
	cfg := testConfig(t)
	env, rest := splitEnv(cfg, nil)
	if env != "local" || len(rest) != 0 {
		t.Errorf("got %q %v", env, rest)
	}
}

func TestSplitArgsStopsAtFirstFlag(t *testing.T) {
	aliases, pass := splitArgs([]string{"api", "worker", "-o", "wide"})
	if !reflect.DeepEqual(aliases, []string{"api", "worker"}) {
		t.Errorf("aliases = %v", aliases)
	}
	if !reflect.DeepEqual(pass, []string{"-o", "wide"}) {
		t.Errorf("passthrough = %v", pass)
	}
}

func TestSplitArgsHonoursDoubleDash(t *testing.T) {
	aliases, pass := splitArgs([]string{"api", "--", "get", "deploy"})
	if !reflect.DeepEqual(aliases, []string{"api"}) {
		t.Errorf("aliases = %v", aliases)
	}
	if !reflect.DeepEqual(pass, []string{"get", "deploy"}) {
		t.Errorf("passthrough = %v", pass)
	}
}

func TestSplitArgsAllAliases(t *testing.T) {
	aliases, pass := splitArgs([]string{"api", "worker"})
	if len(aliases) != 2 || len(pass) != 0 {
		t.Errorf("aliases=%v pass=%v", aliases, pass)
	}
}
