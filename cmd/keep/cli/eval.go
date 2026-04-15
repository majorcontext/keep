package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"
)

// EvalEntry represents a single labeled example in an evaluation dataset.
type EvalEntry struct {
	Input    EvalInput `json:"input"`
	Scope    string    `json:"scope"`
	Expected string    `json:"expected"`
	Label    string    `json:"label"`
}

// EvalInput describes the operation and parameters for an evaluation entry.
type EvalInput struct {
	Operation string         `json:"operation"`
	Params    map[string]any `json:"params"`
}

type evalResult struct {
	expected string
	actual   string
}

type evalMetrics struct {
	Total          int
	Correct        int
	Accuracy       float64
	Precision      float64
	Recall         float64
	FalsePositives int
	FalseNegatives int
}

func parseDataset(path string) ([]EvalEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dataset: %w", err)
	}
	var entries []EvalEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("parse dataset: %w", err)
	}
	return entries, nil
}

func computeMetrics(results []evalResult) evalMetrics {
	var tp, fp, fn, tn int
	for _, r := range results {
		switch {
		case r.expected == "deny" && r.actual == "deny":
			tp++
		case r.expected == "allow" && r.actual == "deny":
			fp++
		case r.expected == "deny" && r.actual == "allow":
			fn++
		case r.expected == "allow" && r.actual == "allow":
			tn++
		}
	}

	total := len(results)
	correct := tp + tn

	var accuracy, precision, recall float64
	if total > 0 {
		accuracy = float64(correct) / float64(total)
	}
	if tp+fp > 0 {
		precision = float64(tp) / float64(tp+fp)
	}
	if tp+fn > 0 {
		recall = float64(tp) / float64(tp+fn)
	}

	return evalMetrics{
		Total:          total,
		Correct:        correct,
		Accuracy:       accuracy,
		Precision:      precision,
		Recall:         recall,
		FalsePositives: fp,
		FalseNegatives: fn,
	}
}

func newEvalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "eval <rules-dir>",
		Short: "Evaluate judge quality against a labeled dataset",
		Args:  cobra.ExactArgs(1),
		RunE:  runEval,
	}

	cmd.Flags().String("dataset", "", "Path to labeled dataset JSON file (required)")
	_ = cmd.MarkFlagRequired("dataset")
	cmd.Flags().String("rule", "", "Evaluate only this rule name")
	cmd.Flags().String("provider", "", "Judge provider to use (e.g. anthropic, openai)")
	cmd.Flags().String("model", "", "Model to use for judge evaluation")
	cmd.Flags().Int("concurrency", 1, "Number of concurrent evaluations")
	cmd.Flags().Duration("timeout", 30*time.Second, "Timeout per evaluation")
	cmd.Flags().String("output", "text", "Output format: text or json")

	return cmd
}

func init() {
	rootCmd.AddCommand(newEvalCmd())
}

func runEval(cmd *cobra.Command, args []string) error {
	datasetPath, err := cmd.Flags().GetString("dataset")
	if err != nil {
		return err
	}
	outputFmt, err := cmd.Flags().GetString("output")
	if err != nil {
		return err
	}

	entries, err := parseDataset(datasetPath)
	if err != nil {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Error:", err)
		return err
	}

	// TODO: Load engine from args[0] (rules dir), configure judge provider,
	// and run each dataset entry through evaluation. For now, report dataset
	// stats and exit.

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Loaded %d entries from dataset\n", len(entries))
	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Rules dir: %s\n", args[0])

	if outputFmt == "json" {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Output format: json\n")
	}

	// TODO: Iterate entries, call engine.Evaluate for each, collect evalResults,
	// then compute and print metrics via computeMetrics().

	_, _ = fmt.Fprintln(cmd.OutOrStdout(), "Live evaluation not yet implemented — use 'keep test' for fixture-based testing")
	return nil
}
