package deps

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/internal/cmdconfig"
	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/project"
)

var (
	syncCfg = &syncConfig{}
	SyncCmd = &cobra.Command{
		Use:   "sync",
		Short: "Reconcile the project and project-lock with the imports",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSync(cmd, syncCfg)
		},
	}
)

type syncConfig struct {
	stackPath     string
	replaceUnobin string
}

type syncResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	ProjectFile   string                  `json:"project-file"   ub:"project-file"`
	LockFile      string                  `json:"lock-file"      ub:"lock-file"`
	Direct        int                     `json:"direct"         ub:"direct"`
	Indirect      int                     `json:"indirect"       ub:"indirect"`
	Selected      int                     `json:"selected"       ub:"selected"`
	Files         []filechange.Change     `json:"files"          ub:"files"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

func init() {
	SyncCmd.Flags().String("format", "text", cmdout.FormatHelp())
	SyncCmd.Flags().StringVarP(&syncCfg.stackPath, "path", "p", ".", cmdconfig.DependencyPathHelp)
	SyncCmd.Flags().StringVar(
		&syncCfg.replaceUnobin, "replace-unobin", "", cmdconfig.DependencyReplacementHelp)
}

func runSync(cmd *cobra.Command, cfg *syncConfig) error {
	format, err := cmdout.CommandFormat(cmd)
	if err != nil {
		return err
	}
	options := cmdconfig.ProjectOptions(cfg.stackPath, cfg.replaceUnobin)
	options.ToolOutput = cmdout.ToolOutput(cmd, format)
	result, err := project.SyncDependencies(options)
	if err != nil {
		var files []filechange.Change
		if result != nil {
			files = result.Files
		}
		return cmdout.WriteOperationError(cmd, format, files, err)
	}
	if format.Machine() {
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, syncResult{
			Kind:          "dependency-sync-result",
			FormatVersion: 1,
			ProjectFile:   result.ProjectFile,
			LockFile:      result.LockFile,
			Direct:        result.Direct,
			Indirect:      result.Indirect,
			Selected:      result.Selected,
			Files:         result.Files,
			Diagnostics:   diagnostic.Merge(result.Diagnostics),
		})
	}
	for _, notice := range result.Diagnostics {
		if err := diagnostic.WriteText(cmd.ErrOrStderr(), notice); err != nil {
			return err
		}
	}
	fmt.Fprintf(cmd.ErrOrStderr(),
		"Wrote %s (%d direct, %d indirect) and %s (%d selected)\n",
		result.ProjectFile, result.Direct, result.Indirect,
		result.LockFile, result.Selected)
	return nil
}
