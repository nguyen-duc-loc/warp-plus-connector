package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewRootCommand creates the CLI without opening state or contacting services.
func NewRootCommand(version, commit string) *cobra.Command {
	if version == "" {
		version = "dev"
	}

	root := &cobra.Command{
		Use:   "wpc",
		Short: "A personal CLI for publicly shared WARP Plus key candidates",
		Long: "Collect and inspect publicly shared WARP Plus key " +
			"candidates.\n\nReal validation is unsupported until isolation and " +
			"cleanup are proven.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		CompletionOptions: cobra.CompletionOptions{
			DisableDefaultCmd: true,
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	root.AddCommand(
		&cobra.Command{
			Use:   "version",
			Short: "Show the executable version and supplied commit",
			Args:  cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				label := version
				if commit != "" {
					label += " (" + commit + ")"
				}
				_, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s\n", cmd.Root().Name(), label)
				return err
			},
		},
	)

	addRuntimeCommands(root, version)
	return root
}
