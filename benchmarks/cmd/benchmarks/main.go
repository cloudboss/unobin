package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/benchmarks/internal/evidence"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := newCommand().ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	options := &commandOptions{}
	command := &cobra.Command{
		Use: "benchmarks", Short: "Collect and validate benchmark evidence",
		SilenceErrors: true, SilenceUsage: true,
	}
	command.PersistentFlags().StringVar(&options.repository, "repository", "..",
		"Source repository; input and output paths are relative to this directory")
	command.PersistentFlags().StringVar(&options.tools, "tools", ".",
		"Benchmark-tool module containing the pinned comparison executable")
	command.AddCommand(collectCommand(options), compareCommand(options),
		validateCommand(options), validateRecordCommand(options))
	return command
}

type commandOptions struct {
	repository string
	tools      string
}

func (options *commandOptions) path(path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(options.repository, path)
	}
	return filepath.Abs(path)
}

func collectCommand(options *commandOptions) *cobra.Command {
	collection := evidence.CollectionOptions{}
	command := &cobra.Command{
		Use: "collect", Short: "Run committed workloads and record commands, logs, and inputs",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			collection.Repository = options.repository
			record, err := evidence.Collect(command.Context(), collection)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Collected %s at %s\n",
				collection.OutDir, record.Revision)
			return err
		},
	}
	command.Flags().StringVar(&collection.SpecPath, "spec", "", "Committed workload JSON file")
	command.Flags().StringVar(&collection.OutDir, "out", "", "New record directory")
	command.Flags().StringVar(&collection.FixtureRevision, "fixture-revision", "",
		"Full preparation commit ID; defaults to the current source commit")
	command.Flags().StringArrayVar(&collection.Capabilities, "capability", nil,
		"Measured build capability; repeat for each capability")
	_ = command.MarkFlagRequired("spec")
	_ = command.MarkFlagRequired("out")
	return command
}

func compareCommand(options *commandOptions) *cobra.Command {
	var before, after, out string
	command := &cobra.Command{
		Use: "compare", Short: "Create an immutable comparison from two validated records",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			left, err := options.path(before)
			if err != nil {
				return err
			}
			right, err := options.path(after)
			if err != nil {
				return err
			}
			output, err := options.path(out)
			if err != nil {
				return err
			}
			for _, dir := range []string{left, right} {
				record, _, err := evidence.LoadRecord(dir)
				if err != nil {
					return err
				}
				if err := evidence.AuditRecord(command.Context(), options.repository, record); err != nil {
					return err
				}
			}
			runner, err := evidence.BenchstatRunner(command.Context(), options.tools)
			if err != nil {
				return err
			}
			summary, err := evidence.WriteComparison(command.Context(), left, right, output, runner)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Compared %s: %s\n", out, summary.Assessment)
			return err
		},
	}
	command.Flags().StringVar(&before, "before", "", "Baseline record directory")
	command.Flags().StringVar(&after, "after", "", "After record directory")
	command.Flags().StringVar(&out, "out", "", "New comparison directory")
	for _, name := range []string{"before", "after", "out"} {
		_ = command.MarkFlagRequired(name)
	}
	return command
}

func validateCommand(options *commandOptions) *cobra.Command {
	var comparison string
	command := &cobra.Command{
		Use: "validate", Short: "Check source IDs, raw samples, statistics, and comparison output",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			dir, err := options.path(comparison)
			if err != nil {
				return err
			}
			if err := evidence.AuditComparison(command.Context(), options.repository, dir); err != nil {
				return err
			}
			runner, err := evidence.BenchstatRunner(command.Context(), options.tools)
			if err != nil {
				return err
			}
			summary, err := evidence.ValidateComparison(command.Context(), dir, runner)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Validated %s: %s\n",
				comparison, summary.Assessment)
			return err
		},
	}
	command.Flags().StringVar(&comparison, "comparison", "", "Completed comparison directory")
	_ = command.MarkFlagRequired("comparison")
	return command
}

func validateRecordCommand(options *commandOptions) *cobra.Command {
	var recordDir string
	command := &cobra.Command{
		Use: "validate-record", Short: "Validate a baseline or after record before comparison",
		Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			dir, err := options.path(recordDir)
			if err != nil {
				return err
			}
			record, files, err := evidence.LoadRecord(dir)
			if err != nil {
				return err
			}
			if _, err := evidence.Validate(record, files); err != nil {
				return err
			}
			if err := evidence.AuditRecord(command.Context(), options.repository, record); err != nil {
				return err
			}
			_, err = fmt.Fprintf(command.OutOrStdout(), "Validated record %s at %s\n",
				recordDir, record.Revision)
			return err
		},
	}
	command.Flags().StringVar(&recordDir, "record", "", "Baseline or after record directory")
	_ = command.MarkFlagRequired("record")
	return command
}
