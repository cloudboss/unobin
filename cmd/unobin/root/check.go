package root

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd, checkCfg)
		},
	}
)

type checkConfig struct {
	path          string
	replaceUnobin string
}

func init() {
	CheckCmd.Flags().StringVarP(&checkCfg.path, "path", "p", ".",
		"Path to a Unobin source file or directory.")
	CheckCmd.Flags().StringVar(&checkCfg.replaceUnobin, "replace-unobin", "",
		"Local path to substitute for github.com/cloudboss/unobin so schema checks read it.")
}

func runCheck(cmd *cobra.Command, cfg *checkConfig) error {
	if err := checkSourcePath(cmd, cfg.path, cfg.replaceUnobin); err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), "OK")
	return nil
}

func checkSourcePath(cmd *cobra.Command, path string, replaceUnobin string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return checkSourceDir(cmd, path, replaceUnobin)
	}
	return checkSourceFile(cmd, path, replaceUnobin)
}

func checkSourceDir(cmd *cobra.Command, path string, replaceUnobin string) error {
	factoryPath := filepath.Join(path, "factory.ub")
	if info, err := os.Stat(factoryPath); err == nil && !info.IsDir() {
		return checkSourceFile(cmd, factoryPath, replaceUnobin)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	source := sourceForDir(path)
	if resolve.HasCompositeExports(source) {
		opts, err := checkOptions(cmd, path, path, replaceUnobin)
		if err != nil {
			return err
		}
		opts.Source = source
		return sourcecheck.CheckUBLibrary(source, opts)
	}

	checked := false
	for _, name := range []string{"project.ub", "project-lock.ub"} {
		candidate := filepath.Join(path, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			if _, err := parseAndValidateSource(candidate); err != nil {
				return err
			}
			checked = true
		} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	if checked {
		return nil
	}
	return fmt.Errorf("%s has no checkable Unobin source", path)
}

func checkSourceFile(cmd *cobra.Command, path string, replaceUnobin string) error {
	file, err := parseAndValidateSource(path)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	switch file.Kind {
	case syntax.FileFactory:
		opts, err := checkOptions(cmd, dir, dir, replaceUnobin)
		if err != nil {
			return err
		}
		opts.Source = sourceForDir(dir)
		_, err = sourcecheck.CheckFactoryBody(file.Factory.Body, opts)
		return err
	case syntax.FileLibrary:
		opts, err := checkOptions(cmd, dir, dir, replaceUnobin)
		if err != nil {
			return err
		}
		opts.Source = sourceForDir(dir)
		return sourcecheck.CheckLibraryFile(file.Library, opts)
	case syntax.FileStack, syntax.FileProject, syntax.FileProjectLock:
		return nil
	default:
		return fmt.Errorf("%s has no checkable Unobin source", path)
	}
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
		cmd.ErrOrStderr(), replaceUnobinAbs, cliVersion())
	return sourcecheck.Options{
		ProjectDir:  projectDir,
		Source:      sourceForDir(sourceDir),
		Resolver:    resolver,
		Versions:    repoVersions,
		SchemaCache: sourcecheck.NewSchemaCache(schemaRoots...),
		WarnOut:     cmd.ErrOrStderr(),
	}, nil
}

func sourceForDir(path string) *resolve.Source {
	return &resolve.Source{FS: os.DirFS(path), Path: path}
}
