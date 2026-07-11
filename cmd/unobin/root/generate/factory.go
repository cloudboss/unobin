package generate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/spf13/cobra"
	"golang.org/x/mod/semver"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/filechange"
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
	format, err := commandFormat(cmd)
	if err != nil {
		return err
	}
	output, err := generateFactory(cfg)
	if err != nil {
		return commandFailure(cmd, format, output, err)
	}
	if format.Machine() {
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, factoryGenerationResult{
			Kind:          "factory-generation-result",
			FormatVersion: 1,
			OutputDir:     output.OutDir,
			Files:         output.Files,
			Diagnostics:   diagnostics(),
		})
	}
	for _, change := range output.Files {
		fmt.Fprintf(cmd.OutOrStdout(), "Created %s\n", change.Path)
	}
	return nil
}

func generateFactory(cfg *factoryConfig) (*generationOutput, error) {
	if cfg.output == "" {
		return nil, fmt.Errorf("--output must not be empty")
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
	output := &generationOutput{OutDir: outDir, Files: []filechange.Change{}}

	factoryPath := filepath.Join(outDir, "factory.ub")
	factorySource, err := lang.Canonicalize(factoryPath, []byte(renderFactoryStub()))
	if err != nil {
		return nil, err
	}
	factoryChange, err := filechange.WriteFile(factoryPath, factorySource, 0o644)
	if err != nil {
		return nil, err
	}
	output.Files = append(output.Files, factoryChange)

	project := &deps.Project{}
	if v := CLIVersion(); semver.IsValid(v) {
		project.UnobinVersion = v
	}
	projectPath := filepath.Join(outDir, deps.ProjectFileName)
	projectChange, err := deps.WriteProjectChange(projectPath, project)
	if err != nil {
		return partialFailure(output, err)
	}
	output.Files = append(output.Files, projectChange)
	output.Files, err = filechange.Compose(output.Files)
	if err != nil {
		return output, err
	}
	return output, nil
}

func renderFactoryStub() string {
	return "factory: {description: 'TODO: describe this factory' inputs: {} imports: {} " +
		"data-sources: {} resources: {} actions: {} outputs: {}}\n"
}
