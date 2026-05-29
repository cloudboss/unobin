package generate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	factoryCfg = &factoryConfig{}
	FactoryCmd = &cobra.Command{
		Use:   "factory",
		Short: "Scaffold a new factory",
		Long: `Scaffold a new factory directory.

The generated directory contains a main.ub source file with empty
placeholder blocks the author fills in. The config.ub is operator
supplied per stack; use init-config (when available) or write
it by hand.

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
	FactoryCmd.Flags().StringVarP(&factoryCfg.output, "output", "o", "",
		"Output directory for the generated factory")
	FactoryCmd.Flags().BoolVar(&factoryCfg.force, "force", false,
		"Overwrite files if the output directory already exists")

	_ = FactoryCmd.MarkFlagRequired("output")
}

func runFactory(cmd *cobra.Command, cfg *factoryConfig) error {
	if cfg.output == "" {
		return fmt.Errorf("--output must not be empty")
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

	factoryPath := filepath.Join(cfg.output, "main.ub")
	if err := os.WriteFile(factoryPath, []byte(renderFactoryStub()), 0o644); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", factoryPath)
	return nil
}

func renderFactoryStub() string {
	return `description: 'TODO: describe this factory'

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
