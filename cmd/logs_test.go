package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/aymenkrifa/kmap/internal/config"
)

func TestLogsSingleAliasHasNoPrefix(t *testing.T) {
	cfg := testConfig(t)
	f := &fakeRunner{out: map[string]string{"logs": "hello\n"}}

	var out bytes.Buffer
	if err := runLogs(context.Background(), cfg, f, &out, "local", []string{"api"}, nil, false); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), "[api]") {
		t.Errorf("single alias should not be prefixed: %q", out.String())
	}
	if !strings.Contains(out.String(), "hello") {
		t.Errorf("output missing: %q", out.String())
	}
}

func TestLogsMultipleAliasesArePrefixed(t *testing.T) {
	cfg := testConfig(t)
	f := &fakeRunner{out: map[string]string{"logs": "line\n"}}

	var out bytes.Buffer
	if err := runLogs(context.Background(), cfg, f, &out, "local", []string{"api", "worker"}, nil, false); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	if !strings.Contains(s, "api") || !strings.Contains(s, "worker") {
		t.Errorf("both prefixes expected: %q", s)
	}
}

func TestLogsBuildsDeploymentArgv(t *testing.T) {
	cfg := testConfig(t)
	f := &fakeRunner{out: map[string]string{"logs": ""}}

	var out bytes.Buffer
	if err := runLogs(context.Background(), cfg, f, &out, "prod", []string{"api"}, nil, false); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.calls[0], " ")
	for _, want := range []string{"--context Production", "logs", "-f",
		"deployment/api-gateway", "-n backend"} {
		if !strings.Contains(joined, want) {
			t.Errorf("argv %q missing %q", joined, want)
		}
	}
}

func TestLogsPrettyFormatsJSON(t *testing.T) {
	cfg := testConfig(t)
	line := `{"timestamp":"2026-09-03T19:14:02Z","status":"error","message":"boom"}` + "\n"
	f := &fakeRunner{out: map[string]string{"logs": line}}

	var out bytes.Buffer
	if err := runLogs(context.Background(), cfg, f, &out, "local", []string{"api"}, nil, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "03-09-2026 19:14:02") {
		t.Errorf("pretty output expected: %q", out.String())
	}
}

func TestLogsPassesThroughFlags(t *testing.T) {
	cfg := testConfig(t)
	f := &fakeRunner{out: map[string]string{"logs": ""}}

	var out bytes.Buffer
	if err := runLogs(context.Background(), cfg, f, &out, "local", []string{"api"},
		[]string{"--tail=100"}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(f.calls[0], " "), "--tail=100") {
		t.Errorf("argv missing flag: %v", f.calls[0])
	}
}

func bundleConfig(t *testing.T) *config.Config {
	t.Helper()
	body := `
version: 1
defaults: {environment: local}
environments:
  local: {command: [klocal]}
aliases:
  bundle: {local: {workloads: [api, worker]}}
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

// An alias naming several workloads resolves to a selector covering all of
// them, so logs must follow all of them rather than silently picking the first.
func TestLogsFansOutOverEveryWorkload(t *testing.T) {
	f := &fakeRunner{out: map[string]string{"logs": "x\n"}}

	var out bytes.Buffer
	if err := runLogs(context.Background(), bundleConfig(t), f, &out, "local",
		[]string{"bundle"}, nil, false); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range f.calls {
		for _, a := range c {
			if strings.HasPrefix(a, "deployment/") {
				got = append(got, a)
			}
		}
	}
	want := []string{"deployment/api", "deployment/worker"}
	strings_sort(got)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("followed %v, want %v", got, want)
	}
	// two streams, so each is labelled with the workload it came from
	s := out.String()
	if !strings.Contains(s, "bundle/api") || !strings.Contains(s, "bundle/worker") {
		t.Errorf("expected per-workload labels:\n%s", s)
	}
}

func strings_sort(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

func TestParseLogsFlags(t *testing.T) {
	cases := []struct {
		in     []string
		pretty bool
		rest   []string
	}{
		{[]string{"api"}, false, []string{"api"}},
		{[]string{"api", "-j"}, true, []string{"api"}},
		{[]string{"-j", "prod", "api"}, true, []string{"prod", "api"}},
		{[]string{"api", "--tail=100"}, false, []string{"api", "--tail=100"}},
		{[]string{"api", "-c", "app"}, false, []string{"api", "-c", "app"}},
		{[]string{"api", "--", "-j"}, false, []string{"api", "-j"}},
	}
	for _, c := range cases {
		opts, rest, err := parseLogsFlags(c.in)
		if err != nil {
			t.Fatal(err)
		}
		if opts.pretty != c.pretty || !reflect.DeepEqual(rest, c.rest) {
			t.Errorf("parseLogsFlags(%v) = %v,%v want %v,%v", c.in, opts.pretty, rest, c.pretty, c.rest)
		}
	}
}
