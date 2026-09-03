// Package cmd holds kmap's command-line surface. Each subcommand lives in its
// own file and registers itself in an init function, so adding a command means
// adding a file and nothing else.
package cmd

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/aymenkrifa/kmap/internal/config"
	"github.com/aymenkrifa/kmap/internal/kube"
)

var (
	configPath string
	runner     kube.Runner = kube.ExecRunner{} // tests swap this for a fake
)

var rootCmd = &cobra.Command{
	Use:           "kmap",
	Short:         "Your services, your names, every cluster",
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "",
		"config file (default: $KMAP_CONFIG, else ~/.config/kmap/config.yaml)")
}

// Execute runs the root command and exits with the appropriate status.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(kube.ExitCode(err))
	}
}

// loadConfig reads and validates the config, turning a missing file into an
// actionable message rather than a stat error.
func loadConfig() (*config.Config, error) {
	path := configPath
	if path == "" {
		path = config.DefaultPath()
	}
	cfg, err := config.Load(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no config at %s\nrun: kmap init", path)
		}
		return nil, err
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

// splitEnv consumes a leading environment name if present, else returns the
// configured default. Cobra has no native optional-leading positional, so
// every command shares this.
func splitEnv(cfg *config.Config, args []string) (string, []string) {
	if len(args) > 0 {
		if _, ok := cfg.Environments[args[0]]; ok {
			return args[0], args[1:]
		}
	}
	return cfg.Defaults.Environment, args
}

// splitArgs divides positional aliases from passthrough arguments. Aliases run
// until the first token beginning with "-", or an explicit "--".
func splitArgs(args []string) (aliases, passthrough []string) {
	for i, a := range args {
		if a == "--" {
			return args[:i], args[i+1:]
		}
		if len(a) > 0 && a[0] == '-' {
			return args[:i], args[i:]
		}
	}
	return args, nil
}

// selfFlags declares the flags a command parses for itself. Cobra cannot both
// accept a command's own flags after the positionals (`pods staging api -f 5`)
// and forward unknown flags to kubectl (`pods prod api -o wide`), so commands
// needing both set DisableFlagParsing and declare their flags here instead.
type selfFlags struct {
	bools map[string]*bool
	ints  map[string]*int
}

// parse pulls the declared flags out of args wherever they appear and returns
// everything else in order. A "--" ends kmap's parsing: what follows belongs to
// kubectl even if kmap declares the same flag. --config and --help are always
// understood, since the persistent flag no longer reaches these commands.
func (s selfFlags) parse(args []string) (rest []string, help bool, err error) {
	rest = make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(rest, args[i+1:]...), help, nil
		}
		if a == "-h" || a == "--help" {
			help = true
			continue
		}
		if p, ok := s.bools[a]; ok {
			*p = true
			continue
		}
		name, value, inline := strings.Cut(a, "=")
		takeValue := func() (string, error) {
			if inline {
				return value, nil
			}
			if i+1 >= len(args) {
				return "", fmt.Errorf("%s needs a value", name)
			}
			i++
			return args[i], nil
		}
		if p, ok := s.ints[name]; ok {
			v, err := takeValue()
			if err != nil {
				return nil, help, err
			}
			n, cErr := strconv.Atoi(v)
			if cErr != nil || n <= 0 {
				return nil, help, fmt.Errorf("%s %q: want a positive number", name, v)
			}
			*p = n
			continue
		}
		if name == "--config" {
			v, err := takeValue()
			if err != nil {
				return nil, help, err
			}
			configPath = v
			continue
		}
		rest = append(rest, a)
	}
	return rest, help, nil
}
