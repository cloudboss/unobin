package root

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/cloudboss/unobin/internal/cmdconfig"
	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	projectpkg "github.com/cloudboss/unobin/pkg/project"
)

var (
	checkCfg = &checkConfig{}
	CheckCmd = &cobra.Command{
		Use:   "check",
		Short: "Check Unobin source without compiling it",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd, checkCfg)
		},
	}
)

type checkConfig struct {
	path          string
	replaceUnobin string
}

type checkTarget struct {
	Path        string `json:"path" ub:"path"`
	Type        string `json:"type" ub:"type"`
	diagnostics []diagnostic.Diagnostic
}

type checkResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	OK            bool                    `json:"ok"             ub:"ok"`
	Target        checkTarget             `json:"target"         ub:"target"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

var errCheckNegative = errors.New("check found errors")

func init() {
	CheckCmd.Flags().String("format", "text", cmdout.FormatHelp())
	CheckCmd.Flags().StringVarP(&checkCfg.path, "path", "p", ".",
		"Path to a Unobin source file or directory.")
	CheckCmd.Flags().StringVar(&checkCfg.replaceUnobin, "replace-unobin", "",
		"Local path to substitute for github.com/cloudboss/unobin so schema checks read it.")
}

func runCheck(cmd *cobra.Command, cfg *checkConfig) error {
	formatValue, err := cmd.Flags().GetString("format")
	if err != nil {
		return err
	}
	format, err := cmdout.ParseFormat(formatValue)
	if err != nil {
		return err
	}
	target, checkErr := checkSourcePath(cmd, cfg.path, cfg.replaceUnobin)
	if format == cmdout.FormatText {
		for _, report := range target.diagnostics {
			if err := diagnostic.WriteText(cmd.ErrOrStderr(), report); err != nil {
				return err
			}
		}
		if checkErr != nil {
			return checkErr
		}
		_, err := fmt.Fprintln(cmd.OutOrStdout(), "OK")
		return err
	}
	if target.Type == "" {
		return cmdout.WriteCommandError(
			cmd,
			format,
			target.diagnostics,
			checkCommandFailure(cfg.path, checkErr),
		)
	}
	mapper := checkPathMapper(cfg.path)
	diagnostics := diagnostic.Merge(
		target.diagnostics,
		diagnostic.FromError(checkErr, diagnostic.ConvertOptions{Path: mapper.Display}),
	)
	ok := !hasErrorDiagnostics(diagnostics)
	if err := cmdout.WriteDocument(cmd.OutOrStdout(), format, checkResult{
		Kind:          "check-result",
		FormatVersion: 1,
		OK:            ok,
		Target:        target,
		Diagnostics:   diagnostics,
	}); err != nil {
		return err
	}
	if !ok {
		return cmdout.Reported(errCheckNegative)
	}
	return nil
}

func checkSourcePath(cmd *cobra.Command, path, replacement string) (checkTarget, error) {
	collector := &diagnostic.Collector{}
	options := cmdconfig.ProjectOptions(path, replacement)
	options.ToolOutput = checkToolOutput(cmd)
	target, err := projectpkg.CheckSource(options, collector)
	return checkTarget{
		Path: target.Path, Type: target.Type, diagnostics: collector.Diagnostics(),
	}, err
}

func cleanCheckPath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func checkCommandFailure(path string, err error) error {
	if err == nil {
		err = errors.New("check target could not be identified")
	}
	var pathError *os.PathError
	if errors.As(err, &pathError) {
		return cmdout.FailWithDiagnostics(
			cmdout.CodeIO,
			"could not inspect check target",
			nil,
			[]diagnostic.Diagnostic{{
				Code:     "unobin.io",
				Severity: diagnostic.SeverityError,
				Message:  err.Error(),
				Path:     cleanCheckPath(path),
			}},
		)
	}
	return cmdout.Fail(cmdout.CodeFailed, "check failed", err)
}

func checkPathMapper(path string) diagnostic.PathMapper {
	workingDir, _ := os.Getwd()
	absolute := path
	if !filepath.IsAbs(absolute) {
		absolute = filepath.Join(workingDir, absolute)
	}
	return diagnostic.PathMapper{
		WorkingDir: workingDir,
		Mappings: []diagnostic.PathMapping{{
			AbsoluteRoot: absolute,
			DisplayRoot:  cleanCheckPath(path),
		}},
	}
}

func hasErrorDiagnostics(diagnostics []diagnostic.Diagnostic) bool {
	for _, report := range diagnostics {
		if report.Severity == diagnostic.SeverityError {
			return true
		}
	}
	return false
}

func checkToolOutput(cmd *cobra.Command) io.Writer {
	value, err := cmd.Flags().GetString("format")
	if err == nil {
		format, parseErr := cmdout.ParseFormat(value)
		if parseErr == nil && format.Machine() {
			return io.Discard
		}
	}
	return cmd.ErrOrStderr()
}
