package generate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	ubmoduleCfg = &ubmoduleConfig{}
	UbmoduleCmd = &cobra.Command{
		Use:   "ubmodule",
		Short: "Scaffold a new UB module",
		Long: `Scaffold a new UB module directory.

The generated directory contains a module.ub manifest and one starter
composite type file. Blocks are empty with TODO comment markers; the
author fills them in.

Examples:
  unobin generate ubmodule -o ./greeter
  unobin generate ubmodule -o ./greeter --type greeting`,

		RunE: func(cmd *cobra.Command, args []string) error {
			return runUbmodule(cmd, ubmoduleCfg)
		},
	}
)

type ubmoduleConfig struct {
	output   string
	typeName string
	force    bool
}

func init() {
	UbmoduleCmd.Flags().StringVarP(&ubmoduleCfg.output, "output", "o", "",
		"Output directory for the generated module")
	UbmoduleCmd.Flags().StringVar(&ubmoduleCfg.typeName, "type", "main",
		"Name of the initial composite type to export")
	UbmoduleCmd.Flags().BoolVar(&ubmoduleCfg.force, "force", false,
		"Overwrite files if the output directory already exists")

	_ = UbmoduleCmd.MarkFlagRequired("output")
}

func runUbmodule(cmd *cobra.Command, cfg *ubmoduleConfig) error {
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

	manifestPath := filepath.Join(cfg.output, "module.ub")
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
	return fmt.Sprintf(`description: 'TODO: describe this module'

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
