package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/aymenkrifa/kmap/internal/config"
	"github.com/aymenkrifa/kmap/internal/kube"
	"github.com/aymenkrifa/kmap/internal/registry"
	"github.com/aymenkrifa/kmap/internal/ui"
)

func init() { rootCmd.AddCommand(newPodsCmd()) }

func newPodsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "pods [env] [alias...] [kubectl flags]",
		Short: "One table of your services' pods",
		Long: `One table of your services' pods.

Positional arguments are an optional environment followed by aliases; the first
argument starting with "-" begins the flags forwarded to kubectl. Use -- to
forward a flag kmap also defines.

  kmap pods                       every default alias, default environment
  kmap pods prod api worker       two aliases in prod
  kmap pods prod api -l tier=web  -l is forwarded to kubectl
  kmap pods -w                    refresh in place
  kmap pods staging api -f 5      refresh every 5 seconds
  kmap pods --columns alias,url   choose the columns

Columns: ` + strings.Join(columnNames(), ", ") + `.
Set a lasting default with defaults.columns in your config.`,
		// kmap's own flags may trail the aliases (` + "`pods api -f 5`" + `) while unknown
		// flags must reach kubectl verbatim (`pods api -l app=x`). Cobra cannot do
		// both, so pods parses its own flags and treats the remainder as opaque.
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, rest, err := parsePodsFlags(args)
			if err != nil {
				return err
			}
			if opts.help {
				return cmd.Help()
			}
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			env, rest := splitEnv(cfg, rest)
			aliases, passthrough := splitArgs(rest)
			if opts.watch {
				aliases, opts.interval = takeInterval(cfg, aliases, opts.interval)
			}
			if len(aliases) == 0 {
				aliases = cfg.Defaults.Aliases
			}
			cols := cfg.Defaults.Columns
			if opts.columns != "" {
				cols = strings.Split(opts.columns, ",")
			}
			out := cmd.OutOrStdout()
			if !opts.watch {
				return runPods(cmd.Context(), cfg, runner, out, env, aliases, passthrough, cols)
			}
			return watchPods(cmd.Context(), cfg, runner, out, env, aliases, passthrough, cols, opts.interval)
		},
	}
	// Registered so they appear in --help and completion, though parsePodsFlags
	// is what actually reads them.
	c.Flags().BoolP("watch", "w", false, "refresh in place until interrupted")
	c.Flags().BoolP("follow", "f", false, "synonym for --watch")
	c.Flags().Int("interval", 2, "seconds between refreshes")
	c.Flags().String("columns", "", "comma-separated columns (default: defaults.columns, else a built-in set)")
	return c
}

// podsOpts are the flags pods owns; everything else is kubectl's.
type podsOpts struct {
	watch    bool
	interval int
	columns  string
	help     bool
}

// parsePodsFlags reads the flags pods owns; everything else is kubectl's.
func parsePodsFlags(args []string) (podsOpts, []string, error) {
	opts := podsOpts{interval: 2}
	rest, help, err := selfFlags{
		bools: map[string]*bool{
			"-w": &opts.watch, "--watch": &opts.watch,
			"-f": &opts.watch, "--follow": &opts.watch,
		},
		ints: map[string]*int{"--interval": &opts.interval},
		strs: map[string]*string{"--columns": &opts.columns},
	}.parse(args)
	opts.help = help
	return opts, rest, err
}

// takeInterval consumes a bare trailing number as the refresh interval, so
// `pods staging search -f 5` keeps working. A token that names a real alias
// is never taken, so a numeric alias still wins.
func takeInterval(cfg *config.Config, aliases []string, interval int) ([]string, int) {
	if len(aliases) == 0 {
		return aliases, interval
	}
	last := aliases[len(aliases)-1]
	if _, isAlias := cfg.Aliases[last]; isAlias {
		return aliases, interval
	}
	n, err := strconv.Atoi(last)
	if err != nil || n <= 0 {
		return aliases, interval
	}
	return aliases[:len(aliases)-1], n
}

// stripOutputFlag removes -o/--output from pods passthrough. kmap asks kubectl
// for JSON and renders its own table, so forwarding the user's -o would send the
// flag twice and fail. Reports whether one was dropped, so it can be announced.
func stripOutputFlag(args []string) ([]string, bool) {
	out, dropped := make([]string, 0, len(args)), false
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-o" || a == "--output":
			dropped = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "-") {
				i++ // its value too
			}
		case strings.HasPrefix(a, "-o=") || strings.HasPrefix(a, "--output="):
			dropped = true
		default:
			out = append(out, a)
		}
	}
	return out, dropped
}

func runPods(ctx context.Context, cfg *config.Config, r kube.Runner, w io.Writer,
	env string, aliases, passthrough, colNames []string) error {

	cols, needs, err := resolveColumns(colNames)
	if err != nil {
		return err
	}
	targets, err := registry.ResolveAll(cfg, aliases, env)
	if err != nil {
		return err
	}

	passthrough, dropped := stripOutputFlag(passthrough)
	if dropped {
		fmt.Fprintf(w, "%snote: -o is ignored by kmap pods, which renders its own table%s\n",
			ui.Gray, ui.Reset)
	}

	// Group by namespace so each namespace costs one call, not one per alias.
	byNS := map[string][]registry.Target{}
	for _, t := range targets {
		byNS[t.Namespace] = append(byNS[t.Namespace], t)
	}
	namespaces := make([]string, 0, len(byNS))
	for ns := range byNS {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)

	envDef := cfg.Environments[env]
	er := envRunner{argv: func(args ...string) []string { return kube.Argv(envDef, args...) }}

	pods := map[string]podJSON{}
	for _, ns := range namespaces {
		args := append([]string{"get", "pods", "-n", ns, "-o", "json"}, passthrough...)
		var buf bytes.Buffer
		if err := r.Run(ctx, kube.Argv(envDef, args...), &buf, io.Discard); err != nil {
			return fmt.Errorf("listing pods in %s/%s: %w", env, ns, err)
		}
		var p podJSON
		if err := json.Unmarshal(buf.Bytes(), &p); err != nil {
			return fmt.Errorf("parsing pods in %s/%s: %w", env, ns, err)
		}
		pods[ns] = p
	}

	// Match first, so the deployment list is only fetched for namespaces that
	// actually have an unexplained empty row.
	matched := make([][]podItem, len(targets))
	absentIn := map[string]bool{}
	for i, t := range targets {
		matched[i] = matchPods(pods[t.Namespace], t.Selector)
		if len(matched[i]) == 0 {
			absentIn[t.Namespace] = true
		}
	}

	extra := map[string]*nsData{}
	for _, ns := range namespaces {
		want := needs
		if !absentIn[ns] {
			want &^= needsDeploys // nothing to explain here
		}
		d, err := fetchNSData(ctx, r, er, ns, want)
		if err != nil {
			return err
		}
		extra[ns] = d
	}

	header := fmt.Sprintf("%s%s%s — %d alias(es)", ui.Bold, env, ui.Reset, len(targets))
	if envDef.Protected {
		header += " " + ui.Red + "[protected]" + ui.Reset
	}
	fmt.Fprintf(w, "%s  %s%s%s\n\n", header, ui.Gray, time.Now().Format("15:04:05"), ui.Reset)

	heads := make([]string, len(cols))
	for i, c := range cols {
		heads[i] = c.header
	}
	tb := ui.NewTable(heads...)

	addRow := func(c cell) {
		cells := make([]string, len(cols))
		for i, col := range cols {
			cells[i] = col.render(c)
		}
		tb.AddRow(cells...)
	}

	for i, t := range targets {
		base := cell{target: t, ns: extra[t.Namespace]}
		if len(matched[i]) == 0 {
			addRow(base)
			continue
		}
		for _, p := range matched[i] {
			c := base
			p := p
			c.pod = &p
			c.total = len(p.Status.ContainerStatuses)
			for _, cs := range p.Status.ContainerStatuses {
				if cs.Ready {
					c.ready++
				}
				c.restarts += cs.RestartCount
			}
			addRow(c)
		}
	}
	tb.Render(w)
	return nil
}

// matchPods filters a pod list by a simple label selector: either `key=value`
// or `key in (a,b)`. Anything else matches nothing, and the row shows absent.
func matchPods(p podJSON, selector string) []podItem {
	key, want, in := parseSelector(selector)
	var out []podItem
	for _, item := range p.Items {
		v, ok := item.Metadata.Labels[key]
		if !ok {
			continue
		}
		if (len(in) == 0 && v == want) || contains(in, v) {
			out = append(out, item)
		}
	}
	return out
}

func parseSelector(s string) (key, value string, in []string) {
	if k, rest, ok := strings.Cut(s, " in ("); ok {
		return strings.TrimSpace(k), "", strings.Split(strings.TrimSuffix(rest, ")"), ",")
	}
	k, v, _ := strings.Cut(s, "=")
	return k, v, nil
}

func contains(ss []string, s string) bool {
	for _, x := range ss {
		if strings.TrimSpace(x) == s {
			return true
		}
	}
	return false
}

func colorPhase(phase string, ready, total int) string {
	switch {
	case phase == "Running" && ready == total && total > 0:
		return ui.Green + phase + ui.Reset
	case phase == "Running":
		return ui.Yellow + phase + ui.Reset
	default:
		return ui.Red + phase + ui.Reset
	}
}

func age(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func watchPods(ctx context.Context, cfg *config.Config, r kube.Runner, w io.Writer,
	env string, aliases, passthrough, cols []string, interval int) error {

	for {
		var buf bytes.Buffer
		if err := runPods(ctx, cfg, r, &buf, env, aliases, passthrough, cols); err != nil {
			return err
		}
		fmt.Fprint(w, buf.String())
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(time.Duration(interval) * time.Second):
		}
		ui.ClearLines(w, strings.Count(buf.String(), "\n"))
	}
}
