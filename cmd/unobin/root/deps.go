package root

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/internal/cmdconfig"
	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/project"
)

// DepsCmd is the parent for the dependency-management subcommands.
var DepsCmd = &cobra.Command{
	Use:   "deps",
	Short: "Manage a factory's dependencies",
	Long: `Manage dependency floors in project.ub and selected versions in project-lock.ub.

A factory or UB library writes imports in .ub source. The project records
its direct dependency floors, and project-lock records the versions and source
hashes the compiler should use.`,
}

var (
	depsSyncCfg = &depsSyncConfig{}
	depsSyncCmd = &cobra.Command{
		Use:   "sync",
		Short: "Reconcile the project and project-lock with the imports",
		Args:  cobra.ExactArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsSync(cmd, depsSyncCfg)
		},
	}

	depsListCfg = &depsSyncConfig{}
	depsListCmd = &cobra.Command{
		Use:   "list",
		Short: "List the project-lock dependencies",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsList(cmd, depsListCfg)
		},
	}

	depsVerifyCfg = &depsSyncConfig{}
	depsVerifyCmd = &cobra.Command{
		Use:   "verify",
		Short: "Check the cached dependencies against project-lock",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsVerify(cmd, depsVerifyCfg)
		},
	}

	depsCleanCmd = &cobra.Command{
		Use:   "clean",
		Short: "Remove the cached dependency sources",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsClean(cmd)
		},
	}

	depsGetCfg = &depsSyncConfig{}
	depsGetCmd = &cobra.Command{
		Use:   "get <dependency>[@version]",
		Short: "Add or update a dependency floor and re-pin",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsGet(cmd, depsGetCfg, args[0])
		},
	}
)

type depsSyncConfig struct {
	stackPath     string
	replaceUnobin string
}

type dependencyListResult struct {
	Kind          string                    `json:"kind"           ub:"kind"`
	FormatVersion int                       `json:"format-version" ub:"format-version"`
	Dependencies  []project.DependencyEntry `json:"dependencies"   ub:"dependencies"`
	Diagnostics   []diagnostic.Diagnostic   `json:"diagnostics"    ub:"diagnostics"`
}

type dependencySyncResult struct {
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

type dependencyGetResult struct {
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

type dependencyVerifyResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	OK            bool                    `json:"ok"             ub:"ok"`
	Checked       int                     `json:"checked"        ub:"checked"`
	Mismatches    []deps.VerifyMismatch   `json:"mismatches"     ub:"mismatches"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

type dependencyCacheCleanResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Removed       bool                    `json:"removed"        ub:"removed"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

var errDependencyVerification = errors.New("dependency verification failed")

const (
	depsPathHelp    = "Path to the factory source file or project directory."
	depsReplaceHelp = "Local path to substitute for github.com/cloudboss/unobin so the " +
		"resolver reads from a working tree instead of fetching."
)

func init() {
	addFormatFlag(depsSyncCmd)
	addFormatFlag(depsListCmd)
	addFormatFlag(depsVerifyCmd)
	addFormatFlag(depsCleanCmd)
	addFormatFlag(depsGetCmd)
	depsSyncCmd.Flags().StringVarP(&depsSyncCfg.stackPath, "path", "p", ".", depsPathHelp)
	depsSyncCmd.Flags().StringVar(&depsSyncCfg.replaceUnobin, "replace-unobin", "", depsReplaceHelp)
	depsListCmd.Flags().StringVarP(&depsListCfg.stackPath, "path", "p", ".", depsPathHelp)
	depsVerifyCmd.Flags().StringVarP(&depsVerifyCfg.stackPath, "path", "p", ".", depsPathHelp)
	depsVerifyCmd.Flags().StringVar(
		&depsVerifyCfg.replaceUnobin, "replace-unobin", "", depsReplaceHelp)
	depsGetCmd.Flags().StringVarP(&depsGetCfg.stackPath, "path", "p", ".", depsPathHelp)
	depsGetCmd.Flags().StringVar(&depsGetCfg.replaceUnobin, "replace-unobin", "", depsReplaceHelp)
	DepsCmd.AddCommand(depsSyncCmd, depsListCmd, depsVerifyCmd, depsCleanCmd, depsGetCmd)
}

func dependencyCommandFormat(cmd *cobra.Command) (cmdout.Format, error) {
	value, err := cmd.Flags().GetString("format")
	if err != nil {
		return "", err
	}
	return cmdout.ParseFormat(value)
}

func dependencyToolOutput(cmd *cobra.Command, format cmdout.Format) io.Writer {
	if format.Machine() {
		return io.Discard
	}
	return cmd.ErrOrStderr()
}

func dependencyCommandFailure(
	cmd *cobra.Command,
	format cmdout.Format,
	result *project.DependencyWriteResult,
	err error,
) error {
	if !format.Machine() {
		return err
	}
	if result != nil {
		err = cmdout.WithFiles(err, result.Files)
	}
	return cmdout.WriteCommandError(cmd, format, nil, err)
}

func dependencyDiagnostics(groups ...[]diagnostic.Diagnostic) []diagnostic.Diagnostic {
	return diagnostic.Merge(groups...)
}

// runDepsSync reconciles the project file and project-lock with the
// project's imports. The project holds the floors; sync reads it,
// requires a floor for every imported repository, removes floors for
// repositories no longer imported, then selects versions across the
// dependency graph, walks the imports to pin every remote library, and
// writes both files at the project root.
func runDepsSync(cmd *cobra.Command, cfg *depsSyncConfig) error {
	format, err := dependencyCommandFormat(cmd)
	if err != nil {
		return err
	}
	options := cmdconfig.ProjectOptions(cfg.stackPath, cfg.replaceUnobin)
	options.ToolOutput = dependencyToolOutput(cmd, format)
	result, err := project.SyncDependencies(options)
	if err != nil {
		return dependencyCommandFailure(cmd, format, result, err)
	}
	if format.Machine() {
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, dependencySyncResult{
			Kind:          "dependency-sync-result",
			FormatVersion: 1,
			ProjectFile:   result.ProjectFile,
			LockFile:      result.LockFile,
			Direct:        result.Direct,
			Indirect:      result.Indirect,
			Selected:      result.Selected,
			Files:         result.Files,
			Diagnostics:   dependencyDiagnostics(result.Diagnostics),
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

// runDepsGet resolves a version for one dependency, sets its floor in the
// project, and re-pins. Automatic and prefix queries choose the highest compatible
// candidate graph. An exact query validates one requested floor.
func runDepsGet(cmd *cobra.Command, cfg *depsSyncConfig, arg string) error {
	format, err := dependencyCommandFormat(cmd)
	if err != nil {
		return err
	}
	options := cmdconfig.ProjectOptions(cfg.stackPath, cfg.replaceUnobin)
	options.ToolOutput = dependencyToolOutput(cmd, format)
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
		var result *project.DependencyWriteResult
		if operation != nil {
			result = operation.Write
		}
		return dependencyCommandFailure(cmd, format, result, err)
	}
	if format.Machine() {
		result := operation.Write
		selectedVersion := operation.SelectedVersion
		if selectedVersion == operation.Version {
			selectedVersion = ""
		}
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, dependencyGetResult{
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
			Diagnostics:     dependencyDiagnostics(operation.Diagnostics),
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

// runDepsList prints the project-lock dependencies, one per line, sorted by id.
func runDepsList(cmd *cobra.Command, cfg *depsSyncConfig) error {
	format, err := dependencyCommandFormat(cmd)
	if err != nil {
		return err
	}
	dependencies, err := project.ListDependencies(cfg.stackPath)
	if err != nil {
		return dependencyCommandFailure(cmd, format, nil, err)
	}
	result := dependencyListResult{
		Kind: "dependency-list", FormatVersion: 1,
		Dependencies: dependencies, Diagnostics: dependencyDiagnostics(),
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

// runDepsVerify re-fetches the project-lock UB dependencies and reports any
// whose content no longer matches the recorded hash.
func runDepsVerify(cmd *cobra.Command, cfg *depsSyncConfig) error {
	format, err := dependencyCommandFormat(cmd)
	if err != nil {
		return err
	}
	verified, err := project.VerifyDependencies(
		cmdconfig.ProjectOptions(cfg.stackPath, cfg.replaceUnobin))
	if err != nil {
		return dependencyCommandFailure(cmd, format, nil, err)
	}
	result := dependencyVerifyResult{
		Kind:          "dependency-verify-result",
		FormatVersion: 1,
		OK:            len(verified.Mismatches) == 0,
		Checked:       verified.Checked,
		Mismatches:    verified.Mismatches,
		Diagnostics:   dependencyDiagnostics(),
	}
	if format.Machine() {
		if err := cmdout.WriteDocument(cmd.OutOrStdout(), format, result); err != nil {
			return err
		}
		if !result.OK {
			return cmdout.Reported(errDependencyVerification)
		}
		return nil
	}
	if !result.OK {
		messages := make([]string, 0, len(result.Mismatches))
		for _, mismatch := range result.Mismatches {
			messages = append(messages, fmt.Sprintf(
				"%s: hash mismatch (selected %s, got %s)",
				mismatch.ID, mismatch.ExpectedHash, mismatch.ActualHash,
			))
		}
		return fmt.Errorf("verification failed:\n  %s", strings.Join(messages, "\n  "))
	}
	fmt.Fprintln(cmd.ErrOrStderr(), "all dependencies verified")
	return nil
}

// runDepsClean removes the cached dependency sources, which are shared
// across projects.
func runDepsClean(cmd *cobra.Command) error {
	format, err := dependencyCommandFormat(cmd)
	if err != nil {
		return err
	}
	cleaned, err := project.CleanDependencies(cmdconfig.ProjectOptions("", ""))
	if err != nil {
		return dependencyCommandFailure(cmd, format, nil, err)
	}
	if format.Machine() {
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, dependencyCacheCleanResult{
			Kind:          "dependency-cache-clean-result",
			FormatVersion: 1,
			Removed:       cleaned.Removed,
			Diagnostics:   dependencyDiagnostics(),
		})
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Removed the import cache at %s\n", cleaned.Path)
	return nil
}
