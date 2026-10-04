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
	getCfg = &getConfig{}
	GetCmd = &cobra.Command{
		Use:   "get <dependency>[@version]",
		Short: "Add or update a dependency floor and re-pin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd, getCfg, args[0])
		},
	}
)

type getConfig struct {
	stackPath     string
	replaceUnobin string
}

type getResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Dependency    string                  `json:"dependency"     ub:"dependency"`
	Version       string                  `json:"version"        ub:"version"`
	Indirect      bool                    `json:"indirect"       ub:"indirect"`
	ProjectFile   string                  `json:"project-file"   ub:"project-file"`
	LockFile      string                  `json:"lock-file"      ub:"lock-file"`
	Direct        int                     `json:"direct"         ub:"direct"`
	Selected      int                     `json:"selected"       ub:"selected"`
	Files         []filechange.Change     `json:"files"          ub:"files"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`

	SelectedVersion string `json:"selected-version,omitempty" ub:"selected-version,omitempty"`
}

func init() {
	GetCmd.Flags().String("format", "text", cmdout.FormatHelp())
	GetCmd.Flags().StringVarP(&getCfg.stackPath, "path", "p", ".", cmdconfig.DependencyPathHelp)
	GetCmd.Flags().StringVar(
		&getCfg.replaceUnobin, "replace-unobin", "", cmdconfig.DependencyReplacementHelp)
}

func runGet(cmd *cobra.Command, cfg *getConfig, arg string) error {
	format, err := cmdout.CommandFormat(cmd)
	if err != nil {
		return err
	}
	options := cmdconfig.ProjectOptions(cfg.stackPath, cfg.replaceUnobin)
	options.ToolOutput = cmdout.ToolOutput(cmd, format)
	if format == cmdout.FormatText {
		options.Progress = func(progress project.DependencyProgress) {
			if progress.Err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Skipping %s %s: %s\n",
					progress.Dependency, progress.Version, progress.Err)
				return
			}
			if progress.SelectedVersion != progress.Version {
				fmt.Fprintf(cmd.ErrOrStderr(), "Requested %s floor %s\n",
					progress.Dependency, progress.Version)
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "Using %s %s\n",
				progress.Dependency, progress.SelectedVersion)
		}
	}
	operation, err := project.UpdateDependency(options, arg)
	if err != nil {
		var files []filechange.Change
		if operation != nil && operation.Write != nil {
			files = operation.Write.Files
		}
		return cmdout.WriteOperationError(cmd, format, files, err)
	}
	if format.Machine() {
		result := operation.Write
		selectedVersion := operation.SelectedVersion
		if selectedVersion == operation.Version {
			selectedVersion = ""
		}
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, getResult{
			Kind:            "dependency-get-result",
			FormatVersion:   1,
			Dependency:      operation.Dependency,
			Version:         operation.Version,
			SelectedVersion: selectedVersion,
			Indirect:        operation.Indirect,
			ProjectFile:     result.ProjectFile,
			LockFile:        result.LockFile,
			Direct:          result.Direct,
			Selected:        result.Selected,
			Files:           result.Files,
			Diagnostics:     diagnostic.Merge(operation.Diagnostics),
		})
	}
	result := operation.Write
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
