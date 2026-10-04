package root

import (
	"io"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/internal/cmdconfig"
	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/graphprint"
	projectpkg "github.com/cloudboss/unobin/pkg/project"
)

var (
	printGraphCfg = &printGraphConfig{}
	PrintGraphCmd = &cobra.Command{
		Use:   "print-graph",
		Short: "Print a factory's dependency graph without compiling it",
		Args:  cobra.NoArgs,
		Long: `Print a factory's dependency graph from its source.

Imports are resolved in memory; composite call sites are expanded
into their internal sub-nodes the same way the generated binary's
print-graph subcommand does. The output is intended to match what
the compiled binary would emit.

Examples:
  unobin print-graph
  unobin print-graph -p factory.ub --format dot | dot -Tsvg > graph.svg`,

		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrintGraph(cmd, printGraphCfg)
		},
	}
)

type printGraphConfig struct {
	stackPath     string
	format        string
	replaceUnobin string
}

func init() {
	PrintGraphCmd.Flags().StringVarP(&printGraphCfg.stackPath, "path", "p", ".",
		"Path to the factory source file or directory.")
	PrintGraphCmd.Flags().StringVar(&printGraphCfg.format, "format", "text",
		"Output format: text, json, unobin, dot.")
	PrintGraphCmd.Flags().StringVar(&printGraphCfg.replaceUnobin, "replace-unobin", "",
		"Local path to substitute for github.com/cloudboss/unobin so the "+
			"resolver reads from a working tree.")
}

func runPrintGraph(cmd *cobra.Command, cfg *printGraphConfig) error {
	format, err := graphprint.ParseFormat(cfg.format)
	if err != nil {
		return err
	}
	collector := &diagnostic.Collector{}
	reporter := diagnostic.Reporter(collector)
	toolOutput := io.Writer(io.Discard)
	if !format.Machine() {
		reporter = textDiagnosticReporter{out: cmd.ErrOrStderr()}
		toolOutput = cmd.ErrOrStderr()
	}
	options := cmdconfig.ProjectOptions(cfg.stackPath, cfg.replaceUnobin)
	options.ToolOutput = toolOutput
	dag, name, err := projectpkg.SourceGraph(options, reporter)
	if err != nil {
		if format.Machine() {
			return cmdout.WriteCommandError(
				cmd, cmdout.Format(format), collector.Diagnostics(), err,
			)
		}
		return err
	}
	out := cmd.OutOrStdout()
	switch format {
	case graphprint.FormatText:
		graphprint.Text(out, dag)
	case graphprint.FormatDOT:
		graphprint.DOT(out, dag, name)
	case graphprint.FormatJSON, graphprint.FormatUnobin:
		return cmdout.WriteDocument(
			out,
			cmdout.Format(format),
			graphprint.BuildDocument(dag, name, collector.Diagnostics()),
		)
	}
	return nil
}

type textDiagnosticReporter struct {
	out io.Writer
}

func (r textDiagnosticReporter) Report(d diagnostic.Diagnostic) {
	_ = diagnostic.WriteText(r.out, d)
}
