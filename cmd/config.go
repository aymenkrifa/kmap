package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

func init() { rootCmd.AddCommand(newConfigCmd()) }

func newConfigCmd() *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Inspect the configuration"}

	c.AddCommand(&cobra.Command{
		Use:   "show",
		Short: "Print the config as kmap understands it, defaults applied",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			b, err := yaml.Marshal(cfg)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "# %s\n%s", cfg.Path, b)
			return nil
		},
	})

	c.AddCommand(&cobra.Command{
		Use:   "validate",
		Short: "Check the config and report every problem",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := loadConfig()
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s: ok (%d environments, %d aliases)\n",
				cfg.Path, len(cfg.Environments), len(cfg.Aliases))
			return nil
		},
	})

	return c
}
