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
	gomoduleCfg = &gomoduleConfig{}
	GomoduleCmd = &cobra.Command{
		Use:   "gomodule",
		Short: "Generate a Go module skeleton from a TF provider schema",
		Long: `Generate a Go module from a Terraform provider schema.

The generated Go module contains typed structs with ub tags and
CRUD method stubs for every resource in the provider.

Examples:
  unobin generate gomodule --from tf --provider random --module-path example.com/modules/random
  unobin generate gomodule --from tf --provider aws -o ./aws-module --module-path example.com/modules/aws`,

		RunE: func(cmd *cobra.Command, args []string) error {
			return runGenerate(cmd, gomoduleCfg)
		},
	}
)

type gomoduleConfig struct {
	from            string
	provider        string
	providerVersion string
	output          string
	modulePath      string
	replaceUnobin   string
}

func init() {
	GomoduleCmd.Flags().StringVar(&gomoduleCfg.from, "from", "tf",
		"Schema source")
	GomoduleCmd.Flags().StringVar(&gomoduleCfg.provider, "provider", "",
		"Terraform provider source (e.g., hashicorp/aws, ansible/ansible)")
	GomoduleCmd.Flags().StringVar(&gomoduleCfg.providerVersion, "provider-version", "",
		"Terraform provider version constraint (e.g., \"~> 5.0\")")
	GomoduleCmd.Flags().StringVarP(&gomoduleCfg.output, "output", "o", "",
		"Output directory for the generated Go module")
	GomoduleCmd.Flags().StringVar(&gomoduleCfg.modulePath, "module-path", "",
		"Go module path for go.mod (e.g., example.com/modules/aws)")
	GomoduleCmd.Flags().StringVar(&gomoduleCfg.replaceUnobin, "replace-unobin", "",
		"Local path to substitute for github.com/cloudboss/unobin via a go.mod replace directive")

	_ = GomoduleCmd.MarkFlagRequired("module-path")
}

func runGenerate(cmd *cobra.Command, cfg *gomoduleConfig) error {
	if strings.ToLower(cfg.from) == "tf" && len(cfg.provider) == 0 {
		return fmt.Errorf("--provider is required when --from is 'tf'")
	}
	if len(cfg.modulePath) == 0 {
		return fmt.Errorf("--module-path must not be empty")
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
		ModulePath:    cfg.modulePath,
		ReplaceUnobin: replaceUnobin,
		From:          cfg.from,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "Generated %s Go module at %s (%d resources).\n",
		adapter.Name(), out.ModulePath, out.Resources)

	return nil
}
