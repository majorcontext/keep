package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("version: %s\n", version)
		fmt.Printf("commit:  %s\n", commit)
		fmt.Printf("date:    %s\n", date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
