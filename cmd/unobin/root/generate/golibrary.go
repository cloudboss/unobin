package generate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboss/unobin/pkg/gogen"
	"github.com/cloudboss/unobin/pkg/gogen/tf"
	"github.com/spf13/cobra"
)

var (
	golibraryCfg = &golibraryConfig{}
	GolibraryCmd = &cobra.Command{
		Use:   "golibrary",
		Short: "Generate a Go library skeleton from a TF provider schema",
		Long: `Generate a Go library from a Terraform provider schema.

The generated Go library contains typed structs with ub tags and
CRUD method stubs for every resource in the provider.

Examples:
  unobin generate golibrary --from tf --provider random --go-module-path example.com/libraries/random
  unobin generate golibrary --from tf --provider aws -o ./aws-library --go-module-path example.com/libraries/aws`,

		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerate(cmd, golibraryCfg)
		},
	}
)

type golibraryConfig struct {
	from            string
	provider        string
	providerVersion string
	output          string
	goModulePath    string
	replaceUnobin   string
}

func init() {
	GolibraryCmd.Flags().StringVar(&golibraryCfg.from, "from", "tf",
		"Schema source")
	GolibraryCmd.Flags().StringVar(&golibraryCfg.provider, "provider", "",
		"Terraform provider source (e.g., hashicorp/aws, ansible/ansible)")
	GolibraryCmd.Flags().StringVar(&golibraryCfg.providerVersion, "provider-version", "",
		"Terraform provider version constraint (e.g., \"~> 5.0\")")
	GolibraryCmd.Flags().StringVarP(&golibraryCfg.output, "output", "o", "",
		"Output directory for the generated Go library")
	GolibraryCmd.Flags().StringVar(&golibraryCfg.goModulePath, "go-module-path", "",
		"Go module path for go.mod (e.g., example.com/libraries/aws)")
	GolibraryCmd.Flags().StringVar(&golibraryCfg.replaceUnobin, "replace-unobin", "",
		"Local path to substitute for github.com/cloudboss/unobin via a go.mod replace directive")

	_ = GolibraryCmd.MarkFlagRequired("go-module-path")
}

func runGenerate(cmd *cobra.Command, cfg *golibraryConfig) error {
	if strings.ToLower(cfg.from) == "tf" && len(cfg.provider) == 0 {
		return fmt.Errorf("--provider is required when --from is 'tf'")
	}
	if len(cfg.goModulePath) == 0 {
		return fmt.Errorf("--go-module-path must not be empty")
	}

	replaceUnobin := cfg.replaceUnobin
	if len(replaceUnobin) != 0 {
		abs, err := filepath.Abs(replaceUnobin)
		if err != nil {
			return err
		}
		replaceUnobin = abs
	}

	ctx := cmd.Context()
	adapter := tf.NewAdapter(&tf.CLIFetcher{}, cfg.provider, cfg.providerVersion)

	out, err := gogen.Generate(ctx, adapter, gogen.Input{
		OutDir:        cfg.output,
		ModulePath:    cfg.goModulePath,
		ReplaceUnobin: replaceUnobin,
		From:          cfg.from,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Generated %s Go library at %s (%d resources).\n",
		adapter.Name(), out.ModulePath, out.Resources)

	return nil
}
