package cmd

import (
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/spf13/cobra"

	"github.com/aymenkrifa/kmap/internal/config"
	"github.com/aymenkrifa/kmap/internal/kube"
	"github.com/aymenkrifa/kmap/internal/logfmt"
	"github.com/aymenkrifa/kmap/internal/registry"
	"github.com/aymenkrifa/kmap/internal/ui"
)

func init() { rootCmd.AddCommand(newLogsCmd()) }

func newLogsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "logs [env] <alias>... [kubectl flags]",
		Short: "Tail one or more of your services",
		Long: `Tail one or more of your services.

Several aliases stream into one terminal, each line labelled with the service it
came from. The first argument starting with "-" begins the flags forwarded to
kubectl; use -- to forward a flag kmap also defines.

  kmap logs api              follow one service
  kmap logs api worker auth  follow three at once, colour-labelled
  kmap logs prod api -j      pretty-print JSON log lines
  kmap logs api --tail=100   --tail is forwarded to kubectl`,
		// See selfFlags in root.go for why this command parses its own flags.
		DisableFlagParsing: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			opts, rest, err := parseLogsFlags(args)
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
			if len(aliases) == 0 {
				return fmt.Errorf("which service? aliases: %v", cfg.AliasNames())
			}
			return runLogs(cmd.Context(), cfg, runner, cmd.OutOrStdout(),
				env, aliases, passthrough, opts.pretty)
		},
	}
	// Registered for --help and completion; parseLogsFlags is what reads it.
	c.Flags().BoolP("pretty", "j", false, "render JSON log lines as coloured text")
	return c
}

type logsOpts struct {
	pretty bool
	help   bool
}

// parseLogsFlags reads the flags logs owns; everything else is kubectl's.
func parseLogsFlags(args []string) (logsOpts, []string, error) {
	var opts logsOpts
	rest, help, err := selfFlags{
		bools: map[string]*bool{"-j": &opts.pretty, "--pretty": &opts.pretty},
	}.parse(args)
	opts.help = help
	return opts, rest, err
}

// stream is one `kubectl logs -f` to run: an alias may name several workloads,
// and each needs its own follow.
type stream struct {
	label     string
	workload  string
	namespace string
}

func plan(targets []registry.Target) []stream {
	var out []stream
	for _, t := range targets {
		for _, wl := range t.Workloads {
			label := t.Alias
			if len(t.Workloads) > 1 {
				label = t.Alias + "/" + wl
			}
			out = append(out, stream{label: label, workload: wl, namespace: t.Namespace})
		}
	}
	return out
}

func runLogs(ctx context.Context, cfg *config.Config, r kube.Runner, w io.Writer,
	env string, aliases, passthrough []string, pretty bool) error {

	targets, err := registry.ResolveAll(cfg, aliases, env)
	if err != nil {
		return err
	}
	streams := plan(targets)
	envDef := cfg.Environments[env]
	shared := ui.NewSyncWriter(w)

	var wg sync.WaitGroup
	errs := make(chan error, len(streams))

	for i, s := range streams {
		// logfmt wraps the prefix writer, so the JSON parser never sees a
		// prefix and a formatted line is labelled exactly once.
		var sink io.Writer = shared
		var closers []io.Closer

		if len(streams) > 1 {
			pw := ui.NewPrefixWriter(shared, fmt.Sprintf("%s[%s]%s ", ui.Color(i), s.label, ui.Reset))
			sink, closers = pw, append(closers, pw)
		}
		if pretty {
			lw := logfmt.NewWriter(sink)
			sink, closers = lw, append(closers, lw)
		}

		args := append([]string{"logs", "-f", "deployment/" + s.workload, "-n", s.namespace}, passthrough...)
		argv := kube.Argv(envDef, args...)

		wg.Add(1)
		go func(s stream, argv []string, sink io.Writer, closers []io.Closer) {
			defer wg.Done()
			err := r.Run(ctx, argv, sink, sink)
			for i := len(closers) - 1; i >= 0; i-- {
				closers[i].Close()
			}
			if err != nil && ctx.Err() == nil {
				errs <- fmt.Errorf("%s: %w", s.label, err)
			}
		}(s, argv, sink, closers)
	}

	wg.Wait()
	close(errs)
	// One stream failing does not fail the command; report the first.
	for e := range errs {
		return e
	}
	return nil
}
