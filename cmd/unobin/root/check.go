package root

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/sourcecheck"
	"github.com/spf13/cobra"
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
	addFormatFlag(CheckCmd)
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

func checkSourcePath(
	cmd *cobra.Command,
	path string,
	replaceUnobin string,
) (checkTarget, error) {
	collector := &diagnostic.Collector{}
	target, err := checkSourcePathWithReporter(cmd, path, replaceUnobin, collector)
	target.diagnostics = collector.Diagnostics()
	return target, err
}

func checkSourcePathWithReporter(
	cmd *cobra.Command,
	path string,
	replaceUnobin string,
	reporter diagnostic.Reporter,
) (checkTarget, error) {
	info, err := os.Stat(path)
	if err != nil {
		return checkTarget{}, err
	}
	if info.IsDir() {
		return checkSourceDir(cmd, path, replaceUnobin, reporter)
	}
	return checkSourceFile(cmd, path, replaceUnobin, reporter)
}

func checkSourceDir(
	cmd *cobra.Command,
	path string,
	replaceUnobin string,
	reporter diagnostic.Reporter,
) (checkTarget, error) {
	factoryPath := filepath.Join(path, "factory.ub")
	if info, err := os.Stat(factoryPath); err == nil && !info.IsDir() {
		return checkSourceFile(cmd, factoryPath, replaceUnobin, reporter)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return checkTarget{}, err
	}

	source := sourceForDir(path)
	if resolve.HasCompositeExports(source) {
		target := checkTarget{Path: cleanCheckPath(path), Type: "library"}
		opts, err := checkOptions(cmd, path, path, replaceUnobin, reporter)
		if err != nil {
			return target, err
		}
		opts.Source = source
		return target, sourcecheck.CheckUBLibrary(source, opts)
	}

	target := checkTarget{Path: cleanCheckPath(path), Type: "directory"}
	checked := false
	for _, name := range []string{"project.ub", "project-lock.ub"} {
		candidate := filepath.Join(path, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			if _, err := parseAndValidateSource(candidate); err != nil {
				return target, err
			}
			checked = true
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return target, err
		}
	}
	if checked {
		return target, nil
	}
	return checkTarget{}, fmt.Errorf("%s has no checkable Unobin source", path)
}

func checkSourceFile(
	cmd *cobra.Command,
	path string,
	replaceUnobin string,
	reporter diagnostic.Reporter,
) (checkTarget, error) {
	target := checkTarget{Path: cleanCheckPath(path), Type: checkTypeFromName(path)}
	file, err := parseAndValidateSource(path)
	if err != nil {
		return target, err
	}
	target.Type = checkTypeFromKind(file.Kind)
	dir := filepath.Dir(path)
	switch file.Kind {
	case syntax.FileFactory:
		opts, err := checkOptions(cmd, dir, dir, replaceUnobin, reporter)
		if err != nil {
			return target, err
		}
		opts.Source = sourceForDir(dir)
		_, err = sourcecheck.CheckFactoryBody(file.Factory.Body, opts)
		return target, err
	case syntax.FileLibrary:
		opts, err := checkOptions(cmd, dir, dir, replaceUnobin, reporter)
		if err != nil {
			return target, err
		}
		opts.Source = sourceForDir(dir)
		return target, sourcecheck.CheckLibraryFile(file.Library, opts)
	case syntax.FileStack, syntax.FileProject, syntax.FileProjectLock:
		return target, nil
	default:
		return checkTarget{}, fmt.Errorf("%s has no checkable Unobin source", path)
	}
}

func cleanCheckPath(path string) string {
	return filepath.ToSlash(filepath.Clean(path))
}

func checkTypeFromName(path string) string {
	switch filepath.Base(path) {
	case "factory.ub":
		return "factory"
	case "library.ub":
		return "library"
	case "project.ub":
		return "project"
	case "project-lock.ub":
		return "project-lock"
	default:
		return ""
	}
}

func checkTypeFromKind(kind syntax.FileKind) string {
	switch kind {
	case syntax.FileFactory:
		return "factory"
	case syntax.FileLibrary:
		return "library"
	case syntax.FileStack:
		return "stack"
	case syntax.FileProject:
		return "project"
	case syntax.FileProjectLock:
		return "project-lock"
	default:
		return ""
	}
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

func parseAndValidateSource(path string) (*syntax.File, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	file, err := syntax.ParseSource(path, body)
	if err != nil {
		return nil, err
	}
	if errs := syntax.ValidateFile(file); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return file, nil
}

func checkOptions(
	cmd *cobra.Command,
	projectStart string,
	sourceDir string,
	replaceUnobin string,
	reporter diagnostic.Reporter,
) (sourcecheck.Options, error) {
	projectDir, err := printGraphProjectDir(projectStart)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	project, err := printGraphProject(projectDir)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	var replaceMap map[deps.Dependency]string
	if project != nil {
		if err := deps.CheckReplacementSentinels(project); err != nil {
			return sourcecheck.Options{}, err
		}
		replaceMap = project.Replace
	}
	projectLock, err := printGraphProjectLock(projectDir)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	resolver, err := newCompileResolver(projectDir)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	resolver = compile.WrapProjectLockSources(resolver, projectLock)
	resolver, err = compile.WrapReplaces(resolver, projectDir, replaceUnobin, replaceMap)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	repoVersions, err := compile.ProjectLockVersions(projectDir)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	repoVersions = printGraphReplacedVersions(
		repoVersions, replaceUnobin != "", replaceMap)
	replaceUnobinAbs, err := printGraphUnobinReplace(projectDir, replaceUnobin, replaceMap)
	if err != nil {
		return sourcecheck.Options{}, err
	}
	schemaRoots := compile.UnobinSchemaRoots(
		checkToolOutput(cmd), replaceUnobinAbs, cliVersion())
	return sourcecheck.Options{
		ProjectDir:  projectDir,
		Source:      sourceForDir(sourceDir),
		Resolver:    resolver,
		Versions:    repoVersions,
		SchemaCache: sourcecheck.NewSchemaCache(schemaRoots...),
		Reporter:    reporter,
	}, nil
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

func sourceForDir(path string) *resolve.Source {
	return &resolve.Source{FS: os.DirFS(path), Path: path}
}
