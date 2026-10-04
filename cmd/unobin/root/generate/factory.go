package generate

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/internal/cmdconfig"
	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/codegen"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
)

var (
	factoryCfg = &factoryConfig{}
	FactoryCmd = &cobra.Command{
		Use:   "factory",
		Short: "Scaffold a new factory",
		Args:  cobra.NoArgs,
		Long: `Scaffold a new factory directory.

The generated directory contains a factory.ub source file with empty
placeholder blocks the author fills in. A stack file is operator supplied
per stack; use the compiled factory's schema template command to create
one.

Examples:
  unobin generate factory -o ./my-factory`,

		RunE: func(cmd *cobra.Command, args []string) error {
			return runFactory(cmd, factoryCfg)
		},
	}
)

type factoryConfig struct {
	output string
	force  bool
}

func init() {
	FactoryCmd.Flags().String("format", "text", cmdout.FormatHelp())
	FactoryCmd.Flags().StringVarP(&factoryCfg.output, "output", "o", "",
		"Output directory for the generated factory")
	FactoryCmd.Flags().BoolVar(&factoryCfg.force, "force", false,
		"Overwrite files if the output directory already exists")

	_ = FactoryCmd.MarkFlagRequired("output")
}

func runFactory(cmd *cobra.Command, cfg *factoryConfig) error {
	format, err := cmdout.CommandFormat(cmd)
	if err != nil {
		return err
	}
	output, err := codegen.ScaffoldFactory(codegen.ScaffoldInput{
		OutDir: cfg.output, Force: cfg.force, UnobinVersion: cmdconfig.CLIVersion(),
	})
	if err != nil {
		var files []filechange.Change
		if output != nil {
			files = output.Files
		}
		return cmdout.WriteOperationError(cmd, format, files, err)
	}
	if format.Machine() {
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, factoryGenerationResult{
			Kind:          "factory-generation-result",
			FormatVersion: 1,
			OutputDir:     output.OutDir,
			Files:         output.Files,
			Diagnostics:   diagnostic.Normalize(nil),
		})
	}
	for _, change := range output.Files {
		fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", change.Path)
	}
	return nil
}

type factoryGenerationResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	OutputDir     string                  `json:"output-dir"     ub:"output-dir"`
	Files         []filechange.Change     `json:"files"          ub:"files"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}
