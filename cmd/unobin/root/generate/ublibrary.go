package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/pkg/lang"
)

var (
	ublibraryCfg = &ublibraryConfig{}
	UblibraryCmd = &cobra.Command{
		Use:   "ublibrary",
		Short: "Scaffold a new UB library",
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
	UblibraryCmd.Flags().StringVarP(&ublibraryCfg.output, "output", "o", "",
		"Output directory for the generated library")
	UblibraryCmd.Flags().StringVar(&ublibraryCfg.typeName, "type", "example",
		"Name of the initial composite type to export")
	UblibraryCmd.Flags().BoolVar(&ublibraryCfg.force, "force", false,
		"Overwrite files if the output directory already exists")

	_ = UblibraryCmd.MarkFlagRequired("output")
}

func runUblibrary(cmd *cobra.Command, cfg *ublibraryConfig) error {
	if cfg.output == "" {
		return fmt.Errorf("--output must not be empty")
	}
	if err := validateUblibraryTypeName(cfg.typeName); err != nil {
		return err
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

	typePath := filepath.Join(cfg.output, cfg.typeName+".ub")
	if err := lang.WriteCanonical(typePath, []byte(renderCompositeStub(cfg.typeName))); err != nil {
		return err
	}

	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "Created %s\n", typePath)
	return nil
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
