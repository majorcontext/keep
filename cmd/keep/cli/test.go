package cli

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/majorcontext/keep"
	"github.com/spf13/cobra"
)

var testCmd = &cobra.Command{
	Use:   "test <rules-dir>",
	Short: "Test rules against fixture files",
	Args:  cobra.ExactArgs(1),
	RunE:  runTest,
}

func init() {
	testCmd.Flags().String("fixtures", "", "Path to fixtures file or directory (required)")
	testCmd.MarkFlagRequired("fixtures")
	testCmd.Flags().String("profiles", "", "Path to profiles directory")
	testCmd.Flags().String("packs", "", "Path to starter packs directory")
	rootCmd.AddCommand(testCmd)
}

func runTest(cmd *cobra.Command, args []string) error {
	rulesDir := args[0]

	fixturesPath, err := cmd.Flags().GetString("fixtures")
	if err != nil {
		return err
	}
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
	// Override all scopes to enforce mode so audit_only rules still fire.
	opts = append(opts, keep.WithForceEnforce())

	eng, err := keep.Load(rulesDir, opts...)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
		return err
	}
	defer eng.Close()

	fixtures, err := LoadFixtures(fixturesPath)
	if err != nil {
		fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
		return err
	}

	total := 0
	passed := 0
	failed := 0

	for _, ff := range fixtures {
		fmt.Fprintf(cmd.OutOrStdout(), "%s:\n", filepath.Base(ff.Path))

		for _, tc := range ff.Tests {
			total++

			// Build the call context.
			ts := time.Now()
			if tc.Call.Context != nil && tc.Call.Context.Timestamp != "" {
				parsed, err := time.Parse(time.RFC3339, tc.Call.Context.Timestamp)
				if err != nil {
					fmt.Fprintf(cmd.ErrOrStderr(), "  FAIL  %s\n        invalid timestamp: %v\n", tc.Name, err)
					failed++
					continue
				}
				ts = parsed
			}

			ctx := keep.CallContext{
				AgentID:   "test",
				Timestamp: ts,
			}
			if tc.Call.Context != nil {
				if tc.Call.Context.AgentID != "" {
					ctx.AgentID = tc.Call.Context.AgentID
				}
				if tc.Call.Context.UserID != "" {
					ctx.UserID = tc.Call.Context.UserID
				}
				if tc.Call.Context.Scope != "" {
					ctx.Scope = tc.Call.Context.Scope
				}
				if tc.Call.Context.Direction != "" {
					ctx.Direction = tc.Call.Context.Direction
				}
				if len(tc.Call.Context.Labels) > 0 {
					ctx.Labels = tc.Call.Context.Labels
				}
			}
			// Fall back to file-level scope.
			if ctx.Scope == "" {
				ctx.Scope = ff.Scope
			}

			call := keep.Call{
				Operation: tc.Call.Operation,
				Params:    tc.Call.Params,
				Context:   ctx,
			}

			result, evalErr := eng.Evaluate(call, ctx.Scope)
			if evalErr != nil {
				failed++
				fmt.Fprintf(cmd.OutOrStdout(), "  FAIL  %s\n        error: %v\n", tc.Name, evalErr)
				continue
			}

			// Compare decision (case-insensitive).
			gotDecision := strings.ToLower(string(result.Decision))
			wantDecision := strings.ToLower(tc.Expect.Decision)

			pass := true
			var failReasons []string

			if gotDecision != wantDecision {
				pass = false
				failReasons = append(failReasons, fmt.Sprintf("        expected: %s (rule: %s)\n        got:      %s (rule: %s)",
					wantDecision, tc.Expect.Rule,
					gotDecision, result.Rule))
			}

			if pass && tc.Expect.Rule != "" && result.Rule != tc.Expect.Rule {
				pass = false
				failReasons = append(failReasons, fmt.Sprintf("        expected rule: %s\n        got rule:      %s",
					tc.Expect.Rule, result.Rule))
			}

			if pass && tc.Expect.Message != "" && !strings.Contains(result.Message, tc.Expect.Message) {
				pass = false
				failReasons = append(failReasons, fmt.Sprintf("        expected message to contain: %q\n        got message: %q",
					tc.Expect.Message, result.Message))
			}

			if pass && len(tc.Expect.Mutations) > 0 {
				if len(result.Mutations) != len(tc.Expect.Mutations) {
					pass = false
					failReasons = append(failReasons, fmt.Sprintf("        expected %d mutations, got %d",
						len(tc.Expect.Mutations), len(result.Mutations)))
				} else {
					for i, em := range tc.Expect.Mutations {
						gm := result.Mutations[i]
						if gm.Path != em.Path || gm.Replaced != em.Replaced {
							pass = false
							failReasons = append(failReasons, fmt.Sprintf("        mutation[%d]: expected path=%q replaced=%q, got path=%q replaced=%q",
								i, em.Path, em.Replaced, gm.Path, gm.Replaced))
						}
					}
				}
			}

			if pass {
				passed++
				fmt.Fprintf(cmd.OutOrStdout(), "  PASS  %s\n", tc.Name)
			} else {
				failed++
				fmt.Fprintf(cmd.OutOrStdout(), "  FAIL  %s\n", tc.Name)
				for _, reason := range failReasons {
					fmt.Fprintln(cmd.OutOrStdout(), reason)
				}
			}
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "\n%d tests, %d passed, %d failed\n", total, passed, failed)

	if failed > 0 {
		return fmt.Errorf("%d test(s) failed", failed)
	}
	return nil
}
