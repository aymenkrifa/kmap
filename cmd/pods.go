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
  kmap pods staging api -f 5      refresh every 5 seconds`,
		// kmap's own flags may trail the aliases (`pods api -f 5`) while unknown
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
			out := cmd.OutOrStdout()
			if !opts.watch {
				return runPods(cmd.Context(), cfg, runner, out, env, aliases, passthrough)
			}
			return watchPods(cmd.Context(), cfg, runner, out, env, aliases, passthrough, opts.interval)
		},
	}
	// Registered so they appear in --help and completion, though parsePodsFlags
	// is what actually reads them.
	c.Flags().BoolP("watch", "w", false, "refresh in place until interrupted")
	c.Flags().BoolP("follow", "f", false, "synonym for --watch")
	c.Flags().Int("interval", 2, "seconds between refreshes")
	return c
}

// podsOpts are the flags pods owns; everything else is kubectl's.
type podsOpts struct {
	watch    bool
	interval int
	help     bool
}

// parsePodsFlags pulls kmap's own flags out of the argument list wherever they
// appear, and returns everything else untouched and in order. A "--" ends
// kmap's parsing: what follows is kubectl's, even if kmap defines the same flag.
func parsePodsFlags(args []string) (podsOpts, []string, error) {
	opts := podsOpts{interval: 2}
	rest := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return opts, append(rest, args[i+1:]...), nil
		case a == "-w" || a == "--watch" || a == "-f" || a == "--follow":
			opts.watch = true
		case a == "-h" || a == "--help":
			opts.help = true
		case a == "--interval":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("--interval needs a number of seconds")
			}
			n, err := strconv.Atoi(args[i+1])
			if err != nil || n <= 0 {
				return opts, nil, fmt.Errorf("--interval %q: want a positive number of seconds", args[i+1])
			}
			opts.interval, i = n, i+1
		case strings.HasPrefix(a, "--interval="):
			n, err := strconv.Atoi(strings.TrimPrefix(a, "--interval="))
			if err != nil || n <= 0 {
				return opts, nil, fmt.Errorf("%s: want a positive number of seconds", a)
			}
			opts.interval = n
		case a == "--config":
			if i+1 >= len(args) {
				return opts, nil, fmt.Errorf("--config needs a path")
			}
			configPath, i = args[i+1], i+1
		case strings.HasPrefix(a, "--config="):
			configPath = strings.TrimPrefix(a, "--config=")
		default:
			rest = append(rest, a)
		}
	}
	return opts, rest, nil
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

// podItem is the minimal shape kmap reads from one entry of `get pods -o json`.
type podItem struct {
	Metadata struct {
		Name              string            `json:"name"`
		Labels            map[string]string `json:"labels"`
		CreationTimestamp time.Time         `json:"creationTimestamp"`
	} `json:"metadata"`
	Status struct {
		Phase             string `json:"phase"`
		ContainerStatuses []struct {
			Ready        bool `json:"ready"`
			RestartCount int  `json:"restartCount"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

type podJSON struct {
	Items []podItem `json:"items"`
}

func runPods(ctx context.Context, cfg *config.Config, r kube.Runner, w io.Writer,
	env string, aliases, passthrough []string) error {

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

	pods := map[string]podJSON{}
	envDef := cfg.Environments[env]
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

	header := fmt.Sprintf("%s%s%s — %d alias(es)", ui.Bold, env, ui.Reset, len(targets))
	if envDef.Protected {
		header += " " + ui.Red + "[protected]" + ui.Reset
	}
	fmt.Fprintf(w, "%s  %s%s%s\n\n", header, ui.Gray, time.Now().Format("15:04:05"), ui.Reset)

	tb := ui.NewTable("ALIAS", "WORKLOAD", "READY", "STATUS", "RESTARTS", "AGE")
	for _, t := range targets {
		matched := matchPods(pods[t.Namespace], t.Selector)
		if len(matched) == 0 {
			tb.AddRow(t.Alias, strings.Join(t.Workloads, ","), "—", ui.Gray+"absent"+ui.Reset, "—", "—")
			continue
		}
		for _, p := range matched {
			ready, total, restarts := 0, len(p.Status.ContainerStatuses), 0
			for _, cs := range p.Status.ContainerStatuses {
				if cs.Ready {
					ready++
				}
				restarts += cs.RestartCount
			}
			tb.AddRow(
				t.Alias,
				strings.Join(t.Workloads, ","),
				fmt.Sprintf("%d/%d", ready, total),
				colorPhase(p.Status.Phase, ready, total),
				fmt.Sprint(restarts),
				age(p.Metadata.CreationTimestamp),
			)
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
	env string, aliases, passthrough []string, interval int) error {

	for {
		var buf bytes.Buffer
		if err := runPods(ctx, cfg, r, &buf, env, aliases, passthrough); err != nil {
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
