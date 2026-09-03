package registry

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aymenkrifa/kmap/internal/config"
)

func load(t *testing.T) *config.Config {
	t.Helper()
	body := `
version: 1
defaults: {environment: local, aliases: [api]}
environments:
  local: {command: [klocal]}
  prod:  {context: Production, namespace: platform}
aliases:
  queue: queue
  api:
    local: api-server
    prod:  api-gateway@backend
  bundle:
    local: {workloads: [api, worker]}
  custom:
    local: {workload: edge, selector: "app.kubernetes.io/name=edge"}
`
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestResolveMatrix(t *testing.T) {
	cfg := load(t)
	cases := []struct {
		alias, env             string
		workload, ns, selector string
	}{
		// scalar alias works in every environment
		{"queue", "local", "queue", "default", "app=queue"},
		{"queue", "prod", "queue", "platform", "app=queue"}, // environment namespace inherited
		// per-environment names
		{"api", "local", "api-server", "default", "app=api-server"},
		// workload@namespace beats the environment namespace
		{"api", "prod", "api-gateway", "backend", "app=api-gateway"},
		// explicit selector wins over the default template
		{"custom", "local", "edge", "default", "app.kubernetes.io/name=edge"},
	}
	for _, c := range cases {
		got, err := Resolve(cfg, c.alias, c.env)
		if err != nil {
			t.Errorf("Resolve(%s,%s): %v", c.alias, c.env, err)
			continue
		}
		if got.Workload() != c.workload || got.Namespace != c.ns || got.Selector != c.selector {
			t.Errorf("Resolve(%s,%s) = %+v, want %s/%s %q",
				c.alias, c.env, got, c.ns, c.workload, c.selector)
		}
	}
}

func TestResolveMultipleWorkloads(t *testing.T) {
	got, err := Resolve(load(t), "bundle", "local")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Workloads) != 2 {
		t.Fatalf("workloads = %v", got.Workloads)
	}
	if got.Selector != "app in (api,worker)" {
		t.Errorf("selector = %q, want app in (api,worker)", got.Selector)
	}
}

func TestResolveErrors(t *testing.T) {
	cfg := load(t)

	_, err := Resolve(cfg, "nope", "local")
	if err == nil || !strings.Contains(err.Error(), "unknown alias") {
		t.Errorf("unknown alias: got %v", err)
	}

	_, err = Resolve(cfg, "api", "nowhere")
	if err == nil || !strings.Contains(err.Error(), "unknown environment") {
		t.Errorf("unknown environment: got %v", err)
	}

	// api has no mapping for an environment that exists but it does not cover
	_, err = Resolve(cfg, "bundle", "prod")
	if err == nil || !strings.Contains(err.Error(), "bundle") {
		t.Errorf("missing mapping: got %v", err)
	}
}

func TestResolveSuggestsNearestAlias(t *testing.T) {
	_, err := Resolve(load(t), "aapi", "local")
	if err == nil || !strings.Contains(err.Error(), "did you mean api") {
		t.Errorf("want suggestion, got %v", err)
	}
}

func TestRenderSelectorRejectsUntemplatableMulti(t *testing.T) {
	_, err := RenderSelector("tier={{.Workload}},app={{.Workload}}", []string{"a", "b"})
	if err == nil {
		t.Fatal("want error for a multi-workload selector that is not a simple key=value")
	}
}
