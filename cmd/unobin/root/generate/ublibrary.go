package generate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	ublibraryCfg = &ublibraryConfig{}
	UblibraryCmd = &cobra.Command{
		Use:   "ublibrary",
		Short: "Scaffold a new UB library",
		Long: `Scaffold a new UB library directory.

The generated directory contains a library.ub manifest and one starter
composite type file. Blocks are empty with TODO comment markers; the
author fills them in.

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
	UblibraryCmd.Flags().StringVarP(&ublibraryCfg.output, "output", "o", "",
		"Output directory for the generated library")
	UblibraryCmd.Flags().StringVar(&ublibraryCfg.typeName, "type", "main",
		"Name of the initial composite type to export")
	UblibraryCmd.Flags().BoolVar(&ublibraryCfg.force, "force", false,
		"Overwrite files if the output directory already exists")

	_ = UblibraryCmd.MarkFlagRequired("output")
}

func runUblibrary(cmd *cobra.Command, cfg *ublibraryConfig) error {
	if cfg.output == "" {
		return fmt.Errorf("--output must not be empty")
	}
	if cfg.typeName == "" {
		return fmt.Errorf("--type must not be empty")
	}

	if _, err := os.Stat(cfg.output); err == nil {
		if !cfg.force {
			return fmt.Errorf("output directory %q already exists; pass --force to overwrite",
				cfg.output)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	if err := os.MkdirAll(cfg.output, 0o755); err != nil {
		return err
	}

	manifestPath := filepath.Join(cfg.output, "library.ub")
	typePath := filepath.Join(cfg.output, cfg.typeName+".ub")

	if err := os.WriteFile(manifestPath, []byte(renderManifest(cfg.typeName)), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(typePath, []byte(renderCompositeStub()), 0o644); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Created %s\n", manifestPath)
	fmt.Fprintf(out, "Created %s\n", typePath)
	return nil
}

func renderManifest(typeName string) string {
	return fmt.Sprintf(`description: 'TODO: describe this library'

exports: {
  %s: '%s.ub'
}
`, typeName, typeName)
}

func renderCompositeStub() string {
	return `description: 'TODO: describe this composite type'

inputs: {
  # TODO: declare inputs
}

imports: {
  # TODO: declare imports
}

resources: {
  # TODO: declare resources
}

outputs: {
  # TODO: declare outputs
}
`
}
