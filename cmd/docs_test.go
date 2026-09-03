package cmd

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aymenkrifa/kmap/internal/config"
)

// A catch-all route that answers 200 for every path would otherwise make every
// service look documented.
func TestLooksLikeDocs(t *testing.T) {
	cases := []struct {
		name string
		path string
		body string
		want bool
	}{
		{"swagger ui html", "/docs", `<html><title>Swagger UI</title><div id="swagger-ui">`, true},
		{"redoc html", "/redoc", `<html><body><redoc spec-url="/openapi.json">`, true},
		{"openapi json", "/openapi.json", `{"openapi":"3.1.0","info":{"title":"x"}}`, true},
		{"swagger 2 json", "/api-docs", `{"swagger":"2.0","info":{}}`, true},
		{"spa catch-all", "/docs", `<html><body><div id="root"></div></body></html>`, false},
		{"health json at a json path", "/openapi.json", `{"status":"ok","version":"1.2.0"}`, false},
		{"not json at all", "/openapi.json", `<html>nope</html>`, false},
	}
	for _, c := range cases {
		if got := looksLikeDocs(c.path, []byte(c.body)); got != c.want {
			t.Errorf("%s: looksLikeDocs(%q) = %v, want %v", c.name, c.path, got, c.want)
		}
	}
}

func TestDocsURLHangsPathOffIngressOrigin(t *testing.T) {
	c := cell{ns: &nsData{ingress: []ingressRoute{
		{service: "api-v2", host: "api.example.com", base: "https://api.example.com",
			url: "https://api.example.com/"},
	}}}
	c.target.Workloads = []string{"api-v2"}

	if got := docsURL(c); got != "" {
		t.Errorf("no docs path configured should give nothing, got %q", got)
	}
	c.target.Docs = "/docs"
	if got := docsURL(c); got != "https://api.example.com/docs" {
		t.Errorf("docsURL = %q", got)
	}
	// an alias with no ingress has nowhere to point
	c.ns = &nsData{}
	if got := docsURL(c); got != "" {
		t.Errorf("no ingress should give nothing, got %q", got)
	}
}

const configWithComments = `version: 1

defaults:
  environment: local

environments:
  local: {command: [klocal]}
  prod: {context: Production}

aliases:
  # same workload in every environment
  queue: queue
  web: web-v2@ui

  # renamed per environment
  api:
    local:   api-v2
    prod:    api-gateway@backend
`

func TestWriteDocsPathsPreservesComments(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(p, []byte(configWithComments), 0o644); err != nil {
		t.Fatal(err)
	}

	n, err := writeDocsPaths(p, "local", []probeResult{
		{alias: "queue", path: "/docs"},
		{alias: "web", path: "/swagger"},
		{alias: "api", path: "/redoc"},
		{alias: "ghost", path: "/docs"}, // not in the file; must be skipped
		{alias: "nope", path: ""},       // nothing found; must be skipped
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Errorf("wrote %d paths, want 3", n)
	}

	out, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	// every line that did not need changing must be byte-identical: this file
	// is maintained by hand, so blank lines and alignment matter too
	before := strings.Split(configWithComments, "\n")
	after := strings.Split(string(out), "\n")
	if len(before) != len(after) {
		t.Fatalf("line count changed %d -> %d:\n%s", len(before), len(after), out)
	}
	changed := 0
	for i := range before {
		if before[i] != after[i] {
			changed++
			if !strings.Contains(after[i], "docs:") {
				t.Errorf("line %d changed but carries no docs path:\n  %q\n  %q",
					i+1, before[i], after[i])
			}
		}
	}
	if changed != 3 {
		t.Errorf("%d lines changed, want exactly 3", changed)
	}
	if _, err := os.Stat(p + ".bak"); err != nil {
		t.Errorf("no backup written: %v", err)
	}

	// and the result must still load, validate, and carry the paths
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatalf("rewritten config does not parse: %v\n%s", err, out)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("rewritten config does not validate: %v\n%s", err, out)
	}
	checks := []struct{ alias, env, wantWorkload, wantDocs, wantNS string }{
		{"queue", "local", "queue", "/docs", ""},
		{"web", "local", "web-v2", "/swagger", "ui"},
		{"api", "local", "api-v2", "/redoc", ""},
	}
	for _, c := range checks {
		m := cfg.Aliases[c.alias].All
		if m == nil {
			m = cfg.Aliases[c.alias].Envs[c.env]
		}
		if m == nil {
			t.Errorf("%s: no mapping after rewrite", c.alias)
			continue
		}
		if m.Workload != c.wantWorkload || m.Docs != c.wantDocs || m.Namespace != c.wantNS {
			t.Errorf("%s = %+v, want workload %q docs %q ns %q",
				c.alias, m, c.wantWorkload, c.wantDocs, c.wantNS)
		}
	}
	// the environment not probed must be untouched
	if prod := cfg.Aliases["api"].Envs["prod"]; prod == nil || prod.Docs != "" ||
		prod.Workload != "api-gateway" || prod.Namespace != "backend" {
		t.Errorf("prod mapping was disturbed: %+v", prod)
	}
}

func TestWriteDocsPathsIsIdempotent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.yaml")
	os.WriteFile(p, []byte(configWithComments), 0o644)

	if _, err := writeDocsPaths(p, "local", []probeResult{{alias: "queue", path: "/docs"}}); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(p)
	// re-running with a different path should replace, not duplicate, the key
	if _, err := writeDocsPaths(p, "local", []probeResult{{alias: "queue", path: "/redoc"}}); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(p)
	if strings.Count(string(second), "docs:") != strings.Count(string(first), "docs:") {
		t.Errorf("docs key duplicated on re-run:\n%s", second)
	}
	cfg, err := config.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Aliases["queue"].All.Docs; got != "/redoc" {
		t.Errorf("docs = %q, want /redoc", got)
	}
}

// "nothing served" hid three different situations. Each should say which.
func TestProbeOutcomeDescribe(t *testing.T) {
	cases := []struct {
		name  string
		o     probeOutcome
		total int
		want  string
	}{
		{"nothing answered at all", probeOutcome{failed: 9}, 9, "unreachable"},
		{"scaled to zero", probeOutcome{codes: map[int]bool{503: true}}, 9, "not serving"},
		{"up but undocumented", probeOutcome{codes: map[int]bool{404: true, 403: true}}, 9, "no documentation path"},
		{"answered but not docs", probeOutcome{codes: map[int]bool{200: true}, rejected: []string{"/api"}}, 9, "200 at /api"},
		{"catch-all answers everything", probeOutcome{codes: map[int]bool{200: true},
			rejected: []string{"/a", "/b", "/c"}}, 3, "catch-all"},
		{"many rejections are truncated", probeOutcome{codes: map[int]bool{200: true},
			rejected: []string{"/a", "/b", "/c", "/d", "/e"}}, 9, "(+2)"},
		{"a 5xx among others is not scaled to zero", probeOutcome{codes: map[int]bool{503: true, 404: true}}, 9, "no documentation path"},
	}
	for _, c := range cases {
		if got := c.o.describe(c.total); !strings.Contains(got, c.want) {
			t.Errorf("%s: describe = %q, want it to mention %q", c.name, got, c.want)
		}
	}
}

func TestProbeDocsRecordsRejections(t *testing.T) {
	// a catch-all origin that answers 200 with an app shell for everything
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/openapi.json" {
			io.WriteString(w, `{"openapi":"3.1.0"}`)
			return
		}
		io.WriteString(w, "<html><div id=root></div></html>")
	}))
	defer srv.Close()

	o := probeDocs(context.Background(), srv.Client(), srv.URL, []string{"/docs", "/openapi.json"})
	if o.match != "/openapi.json" {
		t.Errorf("match = %q, want /openapi.json", o.match)
	}
	if len(o.rejected) != 1 || o.rejected[0] != "/docs" {
		t.Errorf("rejected = %v, want [/docs]", o.rejected)
	}
}

func TestProbeDocsReportsUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	paths := []string{"/docs", "/redoc"}
	o := probeDocs(context.Background(), &http.Client{Timeout: 2 * time.Second}, url, paths)
	if o.match != "" {
		t.Errorf("match = %q, want none", o.match)
	}
	if got := o.describe(len(paths)); got != "unreachable" {
		t.Errorf("describe = %q, want unreachable", got)
	}
}
