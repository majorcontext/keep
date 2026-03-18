package cli

import "github.com/spf13/cobra"

var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "keep",
	Short: "API-level policy engine for AI agents",
}

func Execute() error {
	return rootCmd.Execute()
}
