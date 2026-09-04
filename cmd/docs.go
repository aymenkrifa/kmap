package cmd

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/aymenkrifa/kmap/internal/config"
	"github.com/aymenkrifa/kmap/internal/kube"
	"github.com/aymenkrifa/kmap/internal/registry"
	"github.com/aymenkrifa/kmap/internal/ui"
)

func init() { rootCmd.AddCommand(newDocsCmd()) }

func newDocsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "docs [env] [alias...]",
		Short: "Open or print your services' API documentation",
		Long: `Open or print your services' API documentation.

Each alias may carry a docs path in the config:

  aliases:
    api:
      local: {workload: api-v2, docs: /docs}

kmap hangs that path off the service's ingress origin. Run "kmap docs discover"
once to find the paths and write them in.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDocsList(cmd, args)
		},
	}
	c.Flags().Bool("open", false, "open the documentation in your browser")

	d := &cobra.Command{
		Use:   "discover [env] [alias...]",
		Short: "Probe services for API documentation and record what is found",
		Long: `Probe each alias's ingress origin for the documentation paths in
defaults.docs_paths, and report which ones answer.

With --write the discovered paths are written back into the config. The file is
edited through its parse tree, so comments and formatting survive.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDocsDiscover(cmd, args)
		},
	}
	d.Flags().Bool("write", false, "record the discovered paths in the config file")
	d.Flags().Bool("insecure", false, "skip TLS verification (self-signed dev certs)")
	c.AddCommand(d)

	return c
}

// docsTargets resolves the aliases a docs subcommand was pointed at, along with
// the ingress routes for their namespaces.
func docsTargets(ctx context.Context, cfg *config.Config, args []string) (
	string, []registry.Target, map[string]*nsData, error) {

	env, rest := splitEnv(cfg, args)
	aliases, _ := splitArgs(rest)
	if len(aliases) == 0 {
		aliases = cfg.Defaults.Aliases
	}
	targets, err := registry.ResolveAll(cfg, aliases, env)
	if err != nil {
		return "", nil, nil, err
	}

	envDef := cfg.Environments[env]
	er := envRunner{argv: func(a ...string) []string { return kube.Argv(envDef, a...) }}
	seen := map[string]*nsData{}
	for _, t := range targets {
		if _, ok := seen[t.Namespace]; ok {
			continue
		}
		// pods can shrug off a missing ingress list; docs is built on it.
		d := fetchNSData(ctx, runner, er, t.Namespace, needsIngress)
		if d.missing&needsIngress != 0 {
			return "", nil, nil, fmt.Errorf(
				"cannot list ingresses in %s/%s, and kmap docs needs them to find service URLs", env, t.Namespace)
		}
		seen[t.Namespace] = d
	}
	return env, targets, seen, nil
}

func runDocsList(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	_, targets, ns, err := docsTargets(cmd.Context(), cfg, args)
	if err != nil {
		return err
	}
	open, _ := cmd.Flags().GetBool("open")
	w := cmd.OutOrStdout()

	found := 0
	for _, t := range targets {
		u := docsURL(cell{target: t, ns: ns[t.Namespace]})
		if u == "" {
			continue
		}
		found++
		if open {
			if err := browse(u); err != nil {
				return err
			}
			fmt.Fprintf(w, "opened %s\n", u)
			continue
		}
		fmt.Fprintf(w, "%-14s %s\n", t.Alias, ui.Link(u, u))
	}
	if found == 0 {
		return fmt.Errorf("no alias here has a docs path configured\nrun: kmap docs discover --write")
	}
	return nil
}

func browse(url string) error {
	for _, bin := range []string{"xdg-open", "open"} {
		if p, err := exec.LookPath(bin); err == nil {
			return exec.Command(p, url).Start()
		}
	}
	return fmt.Errorf("no xdg-open or open on PATH; the URL is %s", url)
}

// probeResult is one alias's discovery outcome. When path is empty, note says
// which kind of nothing it was: a service that is down cannot be distinguished
// from one without documentation unless you say so.
type probeResult struct {
	alias string
	env   string
	path  string // the first documentation path that answered, "" if none
	note  string // why nothing was found
}

// probeOutcome is what probing one origin learned.
type probeOutcome struct {
	match    string   // the documentation path that answered
	rejected []string // paths that answered 200 but were not documentation
	codes    map[int]bool
	failed   int // transport-level failures, i.e. nothing answered at all
}

// describe turns an unsuccessful probe into something worth reading.
func (o probeOutcome) describe(total int) string {
	switch {
	case o.failed == total:
		return "unreachable"
	case len(o.rejected) == total:
		// every path answers: a single-page app or a catch-all route, not docs
		return "answers 200 everywhere — catch-all route, not documentation"
	case len(o.rejected) > 0:
		shown := o.rejected
		suffix := ""
		if len(shown) > 3 {
			shown, suffix = shown[:3], fmt.Sprintf(" (+%d)", len(o.rejected)-3)
		}
		return fmt.Sprintf("200 at %s%s, but not documentation", strings.Join(shown, " "), suffix)
	case o.only5xx():
		return "not serving (503) — scaled to zero?"
	default:
		return "no documentation path"
	}
}

func (o probeOutcome) only5xx() bool {
	if len(o.codes) == 0 {
		return false
	}
	for c := range o.codes {
		if c < 500 {
			return false
		}
	}
	return true
}

func runDocsDiscover(cmd *cobra.Command, args []string) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	env, targets, ns, err := docsTargets(cmd.Context(), cfg, args)
	if err != nil {
		return err
	}
	insecure, _ := cmd.Flags().GetBool("insecure")
	insecure = insecure || cfg.Defaults.Insecure
	w := cmd.OutOrStdout()

	client := &http.Client{
		Timeout: 8 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: insecure}, //nolint:gosec // dev clusters use self-signed certs; opt-in only
		},
		// a docs path that redirects to a login page is not documentation
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}

	results := make([]probeResult, 0, len(targets))
	for _, t := range targets {
		r := probeResult{alias: t.Alias, env: env}
		routes := ns[t.Namespace].urlsFor(t.Workloads, nil)
		if len(routes) == 0 {
			r.note = "no ingress"
			results = append(results, r)
			continue
		}
		o := probeDocs(cmd.Context(), client, routes[0].base, cfg.Defaults.DocsPaths)
		r.path = o.match
		if r.path == "" {
			r.note = o.describe(len(cfg.Defaults.DocsPaths))
		}
		results = append(results, r)
	}

	for _, r := range results {
		switch {
		case r.path != "":
			fmt.Fprintf(w, "  %s%-14s %s%s\n", ui.Green, r.alias, r.path, ui.Reset)
		default:
			fmt.Fprintf(w, "  %s%-14s %s%s\n", ui.Gray, r.alias, r.note, ui.Reset)
		}
	}

	write, _ := cmd.Flags().GetBool("write")
	if !write {
		fmt.Fprintf(w, "\n%d of %d have documentation; re-run with --write to record it\n",
			countFound(results), len(results))
		return nil
	}
	n, err := writeDocsPaths(cfg.Path, env, results)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\nwrote %d docs path(s) to %s\n", n, cfg.Path)
	return nil
}

func countFound(rs []probeResult) int {
	n := 0
	for _, r := range rs {
		if r.path != "" {
			n++
		}
	}
	return n
}

// probeDocs looks for the first path that answers 200 with something that is
// actually documentation, and records enough about the failures to explain a
// miss afterwards.
func probeDocs(ctx context.Context, c *http.Client, base string, paths []string) probeOutcome {
	o := probeOutcome{codes: map[int]bool{}}
	for _, p := range paths {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+p, nil)
		if err != nil {
			o.failed++
			continue
		}
		resp, err := c.Do(req)
		if err != nil {
			o.failed++
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		resp.Body.Close()
		o.codes[resp.StatusCode] = true
		if resp.StatusCode != http.StatusOK {
			continue
		}
		if looksLikeDocs(p, body) {
			o.match = p
			return o
		}
		o.rejected = append(o.rejected, p)
	}
	return o
}

// looksLikeDocs guards against a catch-all route answering 200 for everything.
// A JSON path must parse as an OpenAPI document; an HTML one must mention a
// known documentation UI.
func looksLikeDocs(path string, body []byte) bool {
	if strings.HasSuffix(path, ".json") || strings.Contains(path, "api-docs") {
		var doc struct {
			OpenAPI string `json:"openapi"`
			Swagger string `json:"swagger"`
		}
		if err := yaml.Unmarshal(body, &doc); err != nil {
			return false
		}
		return doc.OpenAPI != "" || doc.Swagger != ""
	}
	l := strings.ToLower(string(body))
	for _, marker := range []string{"swagger-ui", "swagger ui", "redoc", "openapi", "scalar", "rapidoc"} {
		if strings.Contains(l, marker) {
			return true
		}
	}
	return false
}

// writeDocsPaths records discovered paths in the config. It rewrites only the
// lines that change, so every comment, blank line and bit of alignment in the
// rest of the file is preserved byte for byte.
func writeDocsPaths(path, env string, results []probeResult) (int, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var root yaml.Node
	if err := yaml.Unmarshal(src, &root); err != nil {
		return 0, err
	}
	doc := &root
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	aliases := mapValue(doc, "aliases")
	if aliases == nil {
		return 0, fmt.Errorf("%s has no aliases block", path)
	}

	type edit struct {
		line int // 1-based line to rewrite
		col  int // 1-based column the value starts at
		text string
	}
	var edits []edit

	for _, r := range results {
		if r.path == "" {
			continue
		}
		node := mapValue(aliases, r.alias)
		if node == nil {
			continue
		}
		// an alias is a bare name, a mapping object, or a map of environments
		target := node
		if node.Kind == yaml.MappingNode && !config.IsMappingShaped(node) {
			e := mapValue(node, env)
			if e == nil {
				continue // this alias does not cover the probed environment
			}
			target = e
		}
		if target.Kind == yaml.MappingNode && target.Style != yaml.FlowStyle {
			// a multi-line block: rewriting it in place would mean deleting a
			// span of lines, so leave it and say so rather than mangle the file
			return len(edits), fmt.Errorf(
				"alias %q is written as a multi-line block; add `docs: %s` to it by hand",
				r.alias, r.path)
		}
		m, err := mappingFromNode(target)
		if err != nil {
			return 0, fmt.Errorf("alias %q: %w", r.alias, err)
		}
		m.Docs = r.path
		text, err := flowMapping(m)
		if err != nil {
			return 0, err
		}
		edits = append(edits, edit{line: target.Line, col: target.Column, text: text})
	}
	if len(edits) == 0 {
		return 0, nil
	}

	lines := strings.Split(string(src), "\n")
	// bottom-up so earlier line numbers stay valid
	sort.Slice(edits, func(i, j int) bool { return edits[i].line > edits[j].line })
	for _, e := range edits {
		if e.line < 1 || e.line > len(lines) {
			return 0, fmt.Errorf("config changed underneath us at line %d", e.line)
		}
		l := lines[e.line-1]
		if e.col-1 > len(l) {
			return 0, fmt.Errorf("config changed underneath us at line %d", e.line)
		}
		lines[e.line-1] = l[:e.col-1] + e.text
	}

	if err := os.WriteFile(path+".bak", src, 0o644); err != nil {
		return 0, err
	}
	return len(edits), os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644)
}

// mappingFromNode reads whatever shape a mapping was written in.
func mappingFromNode(n *yaml.Node) (config.Mapping, error) {
	var m config.Mapping
	err := m.UnmarshalYAML(n)
	return m, err
}

// flowMapping renders a mapping as a single-line { } so it can replace the
// value in place. yaml does the quoting, so a selector with a template in it
// survives.
func flowMapping(m config.Mapping) (string, error) {
	var n yaml.Node
	if err := n.Encode(m); err != nil {
		return "", err
	}
	n.Style = yaml.FlowStyle
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&n); err != nil {
		return "", err
	}
	enc.Close()
	return strings.TrimRight(buf.String(), "\n"), nil
}

func mapValue(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}
