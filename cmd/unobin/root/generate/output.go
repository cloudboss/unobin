package generate

import (
	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/spf13/cobra"
)

type generationOutput struct {
	OutDir string
	Files  []filechange.Change
}

type factoryGenerationResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	OutputDir     string                  `json:"output-dir"     ub:"output-dir"`
	Files         []filechange.Change     `json:"files"          ub:"files"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

type ubLibraryGenerationResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	OutputDir     string                  `json:"output-dir"     ub:"output-dir"`
	Type          string                  `json:"type"           ub:"type"`
	Files         []filechange.Change     `json:"files"          ub:"files"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

type goLibraryGenerationResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	OutputDir     string                  `json:"output-dir"     ub:"output-dir"`
	ModulePath    string                  `json:"module-path"    ub:"module-path"`
	Provider      string                  `json:"provider"       ub:"provider"`
	Resources     int                     `json:"resources"      ub:"resources"`
	DataSources   int                     `json:"data-sources"   ub:"data-sources"`
	Files         []filechange.Change     `json:"files"          ub:"files"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

func commandFormat(cmd *cobra.Command) (cmdout.Format, error) {
	if cmd.Flags().Lookup("format") == nil {
		return cmdout.FormatText, nil
	}
	value, err := cmd.Flags().GetString("format")
	if err != nil {
		return "", err
	}
	return cmdout.ParseFormat(value)
}

func commandFailure(
	cmd *cobra.Command,
	format cmdout.Format,
	output *generationOutput,
	err error,
) error {
	if !format.Machine() {
		return err
	}
	if output != nil {
		err = cmdout.WithFiles(err, output.Files)
	}
	return cmdout.WriteCommandError(cmd, format, nil, err)
}

func diagnostics() []diagnostic.Diagnostic {
	return []diagnostic.Diagnostic{}
}

func partialFailure(output *generationOutput, err error) (*generationOutput, error) {
	if output != nil && len(output.Files) > 0 {
		return output, err
	}
	return nil, err
}
