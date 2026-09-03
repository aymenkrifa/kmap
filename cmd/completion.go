package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

func init() { rootCmd.AddCommand(newCompletionCmd()) }

func newCompletionCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "completion [zsh|bash|fish]",
		Short:     "Generate a shell completion script",
		Args:      cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		ValidArgs: []string{"zsh", "bash", "fish"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "bash":
				return rootCmd.GenBashCompletion(os.Stdout)
			default:
				return rootCmd.GenFishCompletion(os.Stdout, true)
			}
		},
	}
}
