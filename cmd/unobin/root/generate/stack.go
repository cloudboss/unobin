package generate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	stackCfg = &stackConfig{}
	StackCmd = &cobra.Command{
		Use:   "stack",
		Short: "Scaffold a new stack",
		Long: `Scaffold a new stack directory.

The generated directory contains a stack.ub source file with empty
placeholder blocks the author fills in. The config.ub is operator
supplied per deployment; use init-config (when available) or write
it by hand.

Examples:
  unobin generate stack -o ./my-stack`,

		RunE: func(cmd *cobra.Command, args []string) error {
			return runStack(cmd, stackCfg)
		},
	}
)

type stackConfig struct {
	output string
	force  bool
}

func init() {
	StackCmd.Flags().StringVarP(&stackCfg.output, "output", "o", "",
		"Output directory for the generated stack")
	StackCmd.Flags().BoolVar(&stackCfg.force, "force", false,
		"Overwrite files if the output directory already exists")

	_ = StackCmd.MarkFlagRequired("output")
}

func runStack(cmd *cobra.Command, cfg *stackConfig) error {
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

	stackPath := filepath.Join(cfg.output, "stack.ub")
	if err := os.WriteFile(stackPath, []byte(renderStackStub()), 0o644); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", stackPath)
	return nil
}

func renderStackStub() string {
	return `description: 'TODO: describe this stack'

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
