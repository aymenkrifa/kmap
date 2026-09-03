package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/aymenkrifa/kmap/internal/config"
)

func init() { rootCmd.AddCommand(newInitCmd()) }

func newInitCmd() *cobra.Command {
	var force bool
	var kubeconfig string

	c := &cobra.Command{
		Use:   "init",
		Short: "Generate a starter config from your kubeconfig",
		RunE: func(cmd *cobra.Command, _ []string) error {
			kc, err := resolveKubeconfig(kubeconfig)
			if err != nil {
				return err
			}
			out := configPath
			if out == "" {
				out = config.DefaultPath()
			}
			if err := runInit(kc, out, force); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "wrote %s\nadd your aliases, then run: kmap config validate\n", out)
			return nil
		},
	}
	c.Flags().BoolVar(&force, "force", false, "overwrite an existing config")
	c.Flags().StringVar(&kubeconfig, "kubeconfig", "",
		"kubeconfig to read contexts from (default: $KUBECONFIG, else ~/.kube/config)")
	return c
}

// resolveKubeconfig picks the kubeconfig to read, without mutating the flag so
// the choice is not remembered between calls.
func resolveKubeconfig(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if env := os.Getenv("KUBECONFIG"); env != "" {
		return env, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".kube", "config"), nil
}

// kubeconfigContexts is the sliver of a kubeconfig that init reads.
type kubeconfigContexts struct {
	CurrentContext string `yaml:"current-context"`
	Contexts       []struct {
		Name string `yaml:"name"`
	} `yaml:"contexts"`
}

// defaultEnvironment picks what a bare `kmap pods` should target. The first
// context in a kubeconfig is frequently production, so prefer the one kubectl
// is already pointed at, then the first unprotected one, and only then give up
// and take the first.
func (k kubeconfigContexts) defaultEnvironment() string {
	for _, c := range k.Contexts {
		if c.Name == k.CurrentContext {
			return c.Name
		}
	}
	for _, c := range k.Contexts {
		if !looksProtected(c.Name) {
			return c.Name
		}
	}
	return k.Contexts[0].Name
}

// looksProtected reports whether a context name should default to
// protected: true. Anything mentioning prod counts.
func looksProtected(name string) bool {
	return strings.Contains(strings.ToLower(name), "prod")
}

func runInit(kubeconfigPath, outPath string, force bool) error {
	if _, err := os.Stat(outPath); err == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", outPath)
	}

	b, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("reading %s: %w", kubeconfigPath, err)
	}
	var kc kubeconfigContexts
	if err := yaml.Unmarshal(b, &kc); err != nil {
		return fmt.Errorf("parsing %s: %w", kubeconfigPath, err)
	}
	if len(kc.Contexts) == 0 {
		return fmt.Errorf("%s defines no contexts", kubeconfigPath)
	}

	var sb strings.Builder
	sb.WriteString("version: 1\n\n")
	sb.WriteString("defaults:\n")
	fmt.Fprintf(&sb, "  environment: %s\n", kc.defaultEnvironment())
	sb.WriteString("  selector: \"app={{.Workload}}\"\n\n")
	sb.WriteString("environments:\n")
	for _, c := range kc.Contexts {
		fmt.Fprintf(&sb, "  %s:\n    context: %s\n", c.Name, c.Name)
		if looksProtected(c.Name) {
			sb.WriteString("    protected: true\n")
		}
	}
	sb.WriteString(`
# An environment reached by a wrapper rather than a context uses "command:"
# instead, and the wrapper need not be kubectl at all:
#
#   local:
#     command: [klocal]

# aliases map your names onto real workloads. Three shapes:
#
# aliases:
#   queue: queue                         # same workload everywhere
#   api:
#     local: api-server                  # different name per environment
#     prod:  api-gateway@backend         # and a different namespace
#   bundle:
#     local: {workloads: [api, worker]}  # several workloads at once
`)

	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(outPath, []byte(sb.String()), 0o644)
}
