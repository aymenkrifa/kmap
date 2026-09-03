package config

import (
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// A null mapping is what you get from a hand-written `local:` with nothing
// after it. It must be reported, not panicked on.
func TestValidateHandlesNullMapping(t *testing.T) {
	cfg, err := Load(write(t, `
version: 1
defaults: {environment: local}
environments:
  local: {command: [klocal]}
aliases:
  api:
    local:
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	err = cfg.Validate() // must not panic
	if err == nil || !strings.Contains(err.Error(), "api") {
		t.Fatalf("want a complaint naming api, got %v", err)
	}
}

func TestValidateHandlesNullMappingForUndefinedEnvironment(t *testing.T) {
	cfg, err := Load(write(t, `
version: 1
defaults: {environment: local}
environments:
  local: {command: [klocal]}
aliases:
  api:
    nowhere:
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	err = cfg.Validate() // must not panic
	if err == nil || !strings.Contains(err.Error(), "nowhere") {
		t.Fatalf("want a complaint naming nowhere, got %v", err)
	}
}

// `kmap config show` marshals the config back out. What it prints has to be
// something kmap can read again, not a dump of Go struct internals.
func TestMarshalRoundTrips(t *testing.T) {
	cfg, err := Load(write(t, `
version: 1
defaults: {environment: local, aliases: [api]}
environments:
  local: {command: [klocal]}
  prod:  {context: Production, namespace: platform, protected: true}
aliases:
  worker: worker
  api: {local: api-server, prod: api-gateway@backend}
  bundle: {local: {workloads: [api, worker], selector: "x={{.Workload}}"}}
`))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, leak := range []string{"all:", "envs:", "line:"} {
		if strings.Contains(string(out), leak) {
			t.Errorf("marshalled config leaks the internal field %q:\n%s", leak, out)
		}
	}

	back, err := Load(write(t, string(out)))
	if err != nil {
		t.Fatalf("reload: %v\n%s", err, out)
	}
	if err := back.Validate(); err != nil {
		t.Fatalf("reloaded config does not validate: %v\n%s", err, out)
	}

	again, err := yaml.Marshal(back)
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(out) {
		t.Errorf("not a fixed point:\n--- first ---\n%s\n--- second ---\n%s", out, again)
	}

	// the mappings survived intact
	if got := back.Aliases["api"].Envs["prod"]; got == nil ||
		got.Workload != "api-gateway" || got.Namespace != "backend" {
		t.Errorf("api/prod = %+v", got)
	}
	if got := back.Aliases["worker"].All; got == nil || got.Workload != "worker" {
		t.Errorf("worker = %+v", got)
	}
	if got := back.Aliases["bundle"].Envs["local"]; got == nil ||
		len(got.Workloads) != 2 || got.Selector != "x={{.Workload}}" {
		t.Errorf("bundle/local = %+v", got)
	}
	if !back.Environments["prod"].Protected {
		t.Error("prod lost its protected flag")
	}
}
