package cli

import (
	"fmt"
	"strings"

	"github.com/majorcontext/keep"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate <rules-dir>",
	Short: "Validate rule files, profiles, and starter packs",
	Args:  cobra.ExactArgs(1),
	RunE:  runValidate,
}

func init() {
	validateCmd.Flags().String("profiles", "", "Path to profiles directory")
	validateCmd.Flags().String("packs", "", "Path to starter packs directory")
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) error {
	rulesDir := args[0]

	profilesDir, err := cmd.Flags().GetString("profiles")
	if err != nil {
		return err
	}
	packsDir, err := cmd.Flags().GetString("packs")
	if err != nil {
		return err
	}

	var opts []keep.Option
	if profilesDir != "" {
		opts = append(opts, keep.WithProfilesDir(profilesDir))
	}
	if packsDir != "" {
		opts = append(opts, keep.WithPacksDir(packsDir))
	}

	eng, err := keep.Load(rulesDir, opts...)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
		return err
	}
	defer eng.Close()

	scopes := eng.Scopes()
	fmt.Fprintf(cmd.OutOrStdout(), "OK (%d scopes, %s: 0 errors)\n",
		len(scopes), strings.Join(scopes, ", "))
	return nil
}
