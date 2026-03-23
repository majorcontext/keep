package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		w := cmd.OutOrStdout()
		_, _ = fmt.Fprintf(w, "version: %s\n", version)
		_, _ = fmt.Fprintf(w, "commit:  %s\n", commit)
		_, _ = fmt.Fprintf(w, "date:    %s\n", date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
