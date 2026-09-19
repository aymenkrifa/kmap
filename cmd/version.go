package cmd

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// version is overridden at release time with -ldflags "-X ...cmd.version=v1.2.3".
var version = ""

func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "dev"
}

func init() { rootCmd.AddCommand(newVersionCmd()) }

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the kmap version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), resolveVersion())
			return nil
		},
	}
}
