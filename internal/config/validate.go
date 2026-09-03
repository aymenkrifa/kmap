package config

import (
	"fmt"
	"sort"
	"strings"
)

// Problem is one thing wrong with the config file.
type Problem struct {
	Line int
	Msg  string
}

// ValidationError carries every problem found, so a person fixing a
// hand-written file sees all of them in one run rather than one per attempt.
type ValidationError struct {
	Path     string
	Problems []Problem
}

func (e *ValidationError) Error() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s: %d problem(s)\n", e.Path, len(e.Problems))
	for _, p := range e.Problems {
		if p.Line > 0 {
			fmt.Fprintf(&b, "  line %d: %s\n", p.Line, p.Msg)
		} else {
			fmt.Fprintf(&b, "  %s\n", p.Msg)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// Validate checks the whole file and returns a *ValidationError listing every
// problem, or nil.
func (c *Config) Validate() error {
	var ps []Problem
	add := func(line int, format string, args ...any) {
		ps = append(ps, Problem{Line: line, Msg: fmt.Sprintf(format, args...)})
	}

	if c.Version != 1 {
		add(0, "unsupported version %d: kmap understands version 1", c.Version)
	}
	if len(c.Environments) == 0 {
		add(0, "no environments defined")
	}
	if len(c.Aliases) == 0 {
		add(0, "no aliases defined")
	}

	for _, name := range c.EnvNames() {
		env := c.Environments[name]
		hasCmd, hasCtx := len(env.Command) > 0, env.Context != ""
		switch {
		case hasCmd && hasCtx:
			add(0, "environment %q sets both command and context: pick one", name)
		case !hasCmd && !hasCtx:
			add(0, "environment %q sets neither command nor context", name)
		}
	}

	if e := c.Defaults.Environment; e != "" {
		if _, ok := c.Environments[e]; !ok {
			add(0, "defaults.environment %q is not a defined environment (have: %s)",
				e, strings.Join(c.EnvNames(), ", "))
		}
	} else {
		add(0, "defaults.environment is unset and there is more than one environment")
	}

	for _, a := range c.Defaults.Aliases {
		if _, ok := c.Aliases[a]; !ok {
			add(0, "defaults.aliases names %q, which is not a defined alias", a)
		}
	}

	for _, name := range c.AliasNames() {
		al := c.Aliases[name]
		if al.All == nil && len(al.Envs) == 0 {
			add(al.Line, "alias %q defines no mapping", name)
			continue
		}
		check := func(envName string, m *Mapping) {
			if len(m.WorkloadList()) == 0 {
				add(m.Line, "alias %q has no workload for %s", name, envName)
			}
		}
		if al.All != nil {
			check("every environment", al.All)
		}
		envNames := make([]string, 0, len(al.Envs))
		for e := range al.Envs {
			envNames = append(envNames, e)
		}
		sort.Strings(envNames)
		for _, e := range envNames {
			if _, ok := c.Environments[e]; !ok {
				add(al.Envs[e].Line, "alias %q maps environment %q, which is not defined", name, e)
				continue
			}
			check(e, al.Envs[e])
		}
	}

	if len(ps) == 0 {
		return nil
	}
	return &ValidationError{Path: c.Path, Problems: ps}
}
