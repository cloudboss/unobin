package generate

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/codegen"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
)

var (
	ublibraryCfg = &ublibraryConfig{}
	UblibraryCmd = &cobra.Command{
		Use:   "ublibrary",
		Short: "Scaffold a new UB library",
		Args:  cobra.NoArgs,
		Long: `Scaffold a new UB library directory.

The generated directory contains one starter resource composite export
file named <type>.ub. The directory listing is the project, so there is
no separate project file. Blocks are empty for the author to fill in.

Examples:
  unobin generate ublibrary -o ./greeter
  unobin generate ublibrary -o ./greeter --type greeting`,

		RunE: func(cmd *cobra.Command, args []string) error {
			return runUblibrary(cmd, ublibraryCfg)
		},
	}
)

type ublibraryConfig struct {
	output   string
	typeName string
	force    bool
}

func init() {
	UblibraryCmd.Flags().String("format", "text", cmdout.FormatHelp())
	UblibraryCmd.Flags().StringVarP(&ublibraryCfg.output, "output", "o", "",
		"Output directory for the generated library")
	UblibraryCmd.Flags().StringVar(&ublibraryCfg.typeName, "type", "example",
		"Name of the initial composite type to export")
	UblibraryCmd.Flags().BoolVar(&ublibraryCfg.force, "force", false,
		"Overwrite files if the output directory already exists")

	_ = UblibraryCmd.MarkFlagRequired("output")
}

func runUblibrary(cmd *cobra.Command, cfg *ublibraryConfig) error {
	format, err := cmdout.CommandFormat(cmd)
	if err != nil {
		return err
	}
	output, err := codegen.ScaffoldLibrary(codegen.ScaffoldInput{
		OutDir: cfg.output, Force: cfg.force, TypeName: cfg.typeName,
	})
	if err != nil {
		var files []filechange.Change
		if output != nil {
			files = output.Files
		}
		return cmdout.WriteOperationError(cmd, format, files, err)
	}
	if format.Machine() {
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, ubLibraryGenerationResult{
			Kind:          "ub-library-generation-result",
			FormatVersion: 1,
			OutputDir:     output.OutDir,
			Type:          cfg.typeName,
			Files:         output.Files,
			Diagnostics:   diagnostic.Normalize(nil),
		})
	}
	for _, change := range output.Files {
		fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", change.Path)
	}
	return nil
}

type ubLibraryGenerationResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	OutputDir     string                  `json:"output-dir"     ub:"output-dir"`
	Type          string                  `json:"type"           ub:"type"`
	Files         []filechange.Change     `json:"files"          ub:"files"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}
