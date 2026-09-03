// Package registry turns an (alias, environment) pair into a concrete
// workload, namespace and label selector.
package registry

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/aymenkrifa/kmap/internal/config"
)

// Target is a resolved alias: everything a command needs to build an argv.
type Target struct {
	Alias     string
	Env       string
	Namespace string
	Selector  string
	Docs      string   // path to this service's API docs, e.g. /docs
	Workloads []string // always at least one entry
}

// Workload returns the primary workload name.
func (t Target) Workload() string {
	if len(t.Workloads) == 0 {
		return ""
	}
	return t.Workloads[0]
}

// simpleSelector matches a selector template of the form `key={{.Workload}}`,
// the only shape that can be widened to `key in (a,b)`.
var simpleSelector = regexp.MustCompile(`^([A-Za-z0-9._/-]+)=\{\{\s*\.Workload\s*\}\}$`)

// RenderSelector expands a selector template for one or more workloads.
func RenderSelector(tmpl string, workloads []string) (string, error) {
	switch {
	case len(workloads) == 0:
		return "", fmt.Errorf("no workloads to build a selector from")
	case len(workloads) == 1:
		t, err := template.New("selector").Parse(tmpl)
		if err != nil {
			return "", fmt.Errorf("selector template %q: %w", tmpl, err)
		}
		var b strings.Builder
		if err := t.Execute(&b, struct{ Workload string }{workloads[0]}); err != nil {
			return "", fmt.Errorf("selector template %q: %w", tmpl, err)
		}
		return b.String(), nil
	default:
		m := simpleSelector.FindStringSubmatch(tmpl)
		if m == nil {
			return "", fmt.Errorf(
				"selector %q cannot cover multiple workloads: set an explicit selector "+
					"on this alias, or use a template of the form key={{.Workload}}", tmpl)
		}
		return fmt.Sprintf("%s in (%s)", m[1], strings.Join(workloads, ",")), nil
	}
}

// Resolve maps one alias in one environment to a Target.
func Resolve(cfg *config.Config, alias, env string) (Target, error) {
	envDef, ok := cfg.Environments[env]
	if !ok {
		return Target{}, fmt.Errorf("unknown environment %q (have: %s)",
			env, strings.Join(cfg.EnvNames(), ", "))
	}
	al, ok := cfg.Aliases[alias]
	if !ok {
		msg := fmt.Sprintf("unknown alias %q (have: %s)", alias, strings.Join(cfg.AliasNames(), ", "))
		if near := nearest(alias, cfg.AliasNames()); near != "" {
			msg += fmt.Sprintf("\ndid you mean %s?", near)
		}
		return Target{}, fmt.Errorf("%s", msg)
	}

	m := al.All
	if m == nil {
		m = al.Envs[env]
	}
	if m == nil {
		covered := make([]string, 0, len(al.Envs))
		for e := range al.Envs {
			covered = append(covered, e)
		}
		sort.Strings(covered)
		return Target{}, fmt.Errorf("alias %q has no mapping for environment %q (covers: %s)",
			alias, env, strings.Join(covered, ", "))
	}

	ns := m.Namespace
	if ns == "" {
		ns = envDef.Namespace
	}
	if ns == "" {
		ns = config.DefaultNamespace
	}

	tmpl := m.Selector
	if tmpl == "" {
		tmpl = cfg.Defaults.Selector
	}
	workloads := m.WorkloadList()
	sel, err := RenderSelector(tmpl, workloads)
	if err != nil {
		return Target{}, fmt.Errorf("alias %q in %s: %w", alias, env, err)
	}

	return Target{
		Alias:     alias,
		Env:       env,
		Namespace: ns,
		Selector:  sel,
		Docs:      m.Docs,
		Workloads: workloads,
	}, nil
}

// ResolveAll resolves several aliases, failing on the first error.
func ResolveAll(cfg *config.Config, aliases []string, env string) ([]Target, error) {
	out := make([]Target, 0, len(aliases))
	for _, a := range aliases {
		t, err := Resolve(cfg, a, env)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

// nearest returns the closest candidate within edit distance 2, or "".
func nearest(s string, candidates []string) string {
	best, bestD := "", 3
	for _, c := range candidates {
		if d := distance(s, c); d < bestD {
			best, bestD = c, d
		}
	}
	return best
}

// distance is Levenshtein, iterative with two rows.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	cur := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}
