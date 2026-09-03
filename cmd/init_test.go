package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aymenkrifa/kmap/internal/config"
)

const fakeKubeconfig = `
apiVersion: v1
current-context: k3s
contexts:
  - name: k3s
    context: {cluster: k3s, user: k3s}
  - name: Production
    context: {cluster: prod-cluster, user: prod-user}
  - name: Staging
    context: {cluster: staging-cluster, user: staging-user}
`

func TestInitWritesEnvironmentPerContext(t *testing.T) {
	dir := t.TempDir()
	kc := filepath.Join(dir, "kubeconfig")
	out := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(kc, []byte(fakeKubeconfig), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := runInit(kc, out, false); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"k3s", "Production", "Staging"} {
		if _, ok := cfg.Environments[want]; !ok {
			t.Errorf("environment %q missing from %v", want, cfg.EnvNames())
		}
	}
	if cfg.Environments["Production"].Context != "Production" {
		t.Errorf("Production should map to its own context")
	}
}

func TestInitMarksProductionProtected(t *testing.T) {
	dir := t.TempDir()
	kc, out := filepath.Join(dir, "kubeconfig"), filepath.Join(dir, "config.yaml")
	os.WriteFile(kc, []byte(fakeKubeconfig), 0o644)

	if err := runInit(kc, out, false); err != nil {
		t.Fatal(err)
	}
	cfg, _ := config.Load(out)
	if !cfg.Environments["Production"].Protected {
		t.Error("a context named Production should be marked protected")
	}
	if cfg.Environments["Staging"].Protected {
		t.Error("Staging should not be protected")
	}
}

func TestInitRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	kc, out := filepath.Join(dir, "kubeconfig"), filepath.Join(dir, "config.yaml")
	os.WriteFile(kc, []byte(fakeKubeconfig), 0o644)
	os.WriteFile(out, []byte("existing"), 0o644)

	err := runInit(kc, out, false)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("want a refusal mentioning --force, got %v", err)
	}

	if err := runInit(kc, out, true); err != nil {
		t.Fatalf("--force should overwrite: %v", err)
	}
}

func TestInitOutputIsValidAfterAddingAnAlias(t *testing.T) {
	dir := t.TempDir()
	kc, out := filepath.Join(dir, "kubeconfig"), filepath.Join(dir, "config.yaml")
	os.WriteFile(kc, []byte(fakeKubeconfig), 0o644)
	if err := runInit(kc, out, false); err != nil {
		t.Fatal(err)
	}

	// the generated file has no aliases yet; appending one must validate
	f, _ := os.OpenFile(out, os.O_APPEND|os.O_WRONLY, 0o644)
	f.WriteString("\naliases:\n  queue: queue\n")
	f.Close()

	cfg, err := config.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("generated config should validate once an alias exists: %v", err)
	}
}

// runInit takes force as an argument, so a test can pass it without the flag
// existing. The flags have to be registered for anyone to actually reach it.
func TestInitRegistersItsFlags(t *testing.T) {
	c := newInitCmd()
	for _, name := range []string{"force", "kubeconfig"} {
		if c.Flags().Lookup(name) == nil {
			t.Errorf("init has no --%s flag", name)
		}
	}
}

// The first context in a kubeconfig is often the production one, and a bare
// `kmap pods` uses defaults.environment. It must not land on prod by accident.
func TestInitDefaultsToCurrentContext(t *testing.T) {
	dir := t.TempDir()
	kc, out := filepath.Join(dir, "kubeconfig"), filepath.Join(dir, "config.yaml")
	os.WriteFile(kc, []byte(fakeKubeconfig), 0o644)
	if err := runInit(kc, out, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.Environment != "k3s" {
		t.Errorf("defaults.environment = %q, want the kubeconfig's current-context k3s",
			cfg.Defaults.Environment)
	}
}

func TestInitAvoidsProtectedDefaultWithoutCurrentContext(t *testing.T) {
	dir := t.TempDir()
	kc, out := filepath.Join(dir, "kubeconfig"), filepath.Join(dir, "config.yaml")
	os.WriteFile(kc, []byte(`
contexts:
  - name: Production
  - name: Staging
`), 0o644)
	if err := runInit(kc, out, false); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(out)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Defaults.Environment != "Staging" {
		t.Errorf("defaults.environment = %q, want the first unprotected context",
			cfg.Defaults.Environment)
	}
}
