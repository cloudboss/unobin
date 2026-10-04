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
	CleanCmd = &cobra.Command{
		Use:   "clean",
		Short: "Remove the cached dependency sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runClean(cmd)
		},
	}
)

type cleanResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Removed       bool                    `json:"removed"        ub:"removed"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

func init() {
	CleanCmd.Flags().String("format", "text", cmdout.FormatHelp())
}

func runClean(cmd *cobra.Command) error {
	format, err := cmdout.CommandFormat(cmd)
	if err != nil {
		return err
	}
	cleaned, err := project.CleanDependencies(cmdconfig.ProjectOptions("", ""))
	if err != nil {
		return cmdout.WriteOperationError(cmd, format, nil, err)
	}
	if format.Machine() {
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, cleanResult{
			Kind:          "dependency-cache-clean-result",
			FormatVersion: 1,
			Removed:       cleaned.Removed,
			Diagnostics:   diagnostic.Merge(),
		})
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Removed the import cache at %s\n", cleaned.Path)
	return nil
}
