package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/lang"
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
	format, err := commandFormat(cmd)
	if err != nil {
		return err
	}
	output, err := generateUBLibrary(cfg)
	if err != nil {
		return commandFailure(cmd, format, output, err)
	}
	if format.Machine() {
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, ubLibraryGenerationResult{
			Kind:          "ub-library-generation-result",
			FormatVersion: 1,
			OutputDir:     output.OutDir,
			Type:          cfg.typeName,
			Files:         output.Files,
			Diagnostics:   diagnostics(),
		})
	}
	for _, change := range output.Files {
		fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", change.Path)
	}
	return nil
}

func generateUBLibrary(cfg *ublibraryConfig) (*generationOutput, error) {
	if cfg.output == "" {
		return nil, fmt.Errorf("--output must not be empty")
	}
	if err := validateUblibraryTypeName(cfg.typeName); err != nil {
		return nil, err
	}
	outDir := filepath.Clean(cfg.output)

	if _, err := os.Stat(outDir); err == nil {
		if !cfg.force {
			return nil, fmt.Errorf(
				"output directory %q already exists; pass --force to overwrite", outDir,
			)
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, err
	}

	typePath := filepath.Join(outDir, cfg.typeName+".ub")
	source, err := lang.Canonicalize(typePath, []byte(renderCompositeStub(cfg.typeName)))
	if err != nil {
		return nil, err
	}
	change, err := filechange.WriteFile(typePath, source, 0o644)
	if err != nil {
		return nil, err
	}
	return &generationOutput{
		OutDir: outDir,
		Files:  []filechange.Change{change},
	}, nil
}

func validateUblibraryTypeName(name string) error {
	if name == "" {
		return fmt.Errorf("--type must not be empty")
	}
	if strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("--type must be a file name, got %q", name)
	}
	switch name {
	case "factory", "main", "project", "project-lock":
		return fmt.Errorf("--type %q is reserved; choose another type name", name)
	}
	return nil
}

func renderCompositeStub(name string) string {
	return fmt.Sprintf("%s: resource {description: 'TODO: describe this composite type' "+
		"inputs: {} imports: {} data-sources: {} resources: {} actions: {} outputs: {}}\n", name)
}
