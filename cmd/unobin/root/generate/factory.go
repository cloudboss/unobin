package generate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/lang"
)

// CLIVersion reports the running CLI's own version; the root command
// assigns it at startup. A release version becomes the toolchain pin
// scaffolds record.
var CLIVersion = func() string { return "dev" }

var (
	factoryCfg = &factoryConfig{}
	FactoryCmd = &cobra.Command{
		Use:   "factory",
		Short: "Scaffold a new factory",
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

	factoryPath := filepath.Join(cfg.output, "factory.ub")
	if err := lang.WriteCanonical(factoryPath, []byte(renderFactoryStub())); err != nil {
		return err
	}

	manifest := &deps.Manifest{}
	if v := CLIVersion(); semver.IsValid(v) {
		manifest.UnobinVersion = v
	}
	manifestPath := filepath.Join(cfg.output, deps.SourceManifestFileName)
	if err := deps.WriteSourceManifest(manifestPath, manifest); err != nil {
		return err
	}
	legacyManifestPath := filepath.Join(cfg.output, deps.ManifestFileName)
	if err := os.Remove(legacyManifestPath); err != nil && !os.IsNotExist(err) {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", factoryPath)
	fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", manifestPath)
	return nil
}

func renderFactoryStub() string {
	return "factory: {description: 'TODO: describe this factory' inputs: {} imports: {} " +
		"data: {} resources: {} actions: {} outputs: {}}\n"
}
