package deps

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/internal/cmdconfig"
	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/project"
)

var (
	listCfg = &listConfig{}
	ListCmd = &cobra.Command{
		Use:   "list",
		Short: "List the project-lock dependencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runList(cmd, listCfg)
		},
	}
)

type listConfig struct {
	stackPath string
}

type listResult struct {
	Kind          string                    `json:"kind"           ub:"kind"`
	FormatVersion int                       `json:"format-version" ub:"format-version"`
	Dependencies  []project.DependencyEntry `json:"dependencies"   ub:"dependencies"`
	Diagnostics   []diagnostic.Diagnostic   `json:"diagnostics"    ub:"diagnostics"`
}

func init() {
	ListCmd.Flags().String("format", "text", cmdout.FormatHelp())
	ListCmd.Flags().StringVarP(&listCfg.stackPath, "path", "p", ".", cmdconfig.DependencyPathHelp)
}

func runList(cmd *cobra.Command, cfg *listConfig) error {
	format, err := cmdout.CommandFormat(cmd)
	if err != nil {
		return err
	}
	dependencies, err := project.ListDependencies(cfg.stackPath)
	if err != nil {
		return cmdout.WriteOperationError(cmd, format, nil, err)
	}
	result := listResult{
		Kind: "dependency-list", FormatVersion: 1,
		Dependencies: dependencies, Diagnostics: diagnostic.Merge(),
	}
	if format.Machine() {
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, result)
	}
	out := cmd.OutOrStdout()
	for _, dependency := range result.Dependencies {
		fmt.Fprintf(
			out, "%s %s (%s)\n", dependency.ID, dependency.Version, dependency.Kind,
		)
	}
	return nil
}
