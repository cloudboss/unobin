package root

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/graphprint"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/sourcecheck"
	"github.com/cloudboss/unobin/pkg/toolchain"
	"github.com/spf13/cobra"
)

var (
	printGraphCfg = &printGraphConfig{}
	PrintGraphCmd = &cobra.Command{
		Use:   "print-graph",
		Short: "Print a factory's dependency graph without compiling it",
		Long: `Print a factory's dependency graph from its source.

Imports are resolved in memory; composite call sites are expanded
into their internal sub-nodes the same way the generated binary's
print-graph subcommand does. The output is intended to match what
the compiled binary would emit.

Examples:
  unobin print-graph
  unobin print-graph -p factory.ub --format dot | dot -Tsvg > graph.svg`,

		RunE: func(cmd *cobra.Command, args []string) error {
			return runPrintGraph(cmd, printGraphCfg)
		},
	}
)

type printGraphConfig struct {
	stackPath     string
	format        string
	replaceUnobin string
}

func init() {
	PrintGraphCmd.Flags().StringVarP(&printGraphCfg.stackPath, "path", "p", ".",
		"Path to the factory source file or directory.")
	PrintGraphCmd.Flags().StringVar(&printGraphCfg.format, "format", "plain",
		"Output format: 'plain' for an indented text listing, 'dot' for Graphviz.")
	PrintGraphCmd.Flags().StringVar(&printGraphCfg.replaceUnobin, "replace-unobin", "",
		"Local path to substitute for github.com/cloudboss/unobin so the "+
			"resolver reads from a working tree.")
}

func runPrintGraph(cmd *cobra.Command, cfg *printGraphConfig) error {
	stackPath, err := compile.FactorySourcePath(cfg.stackPath)
	if err != nil {
		return err
	}
	src, err := os.ReadFile(stackPath)
	if err != nil {
		return err
	}
	sf, _, err := compile.ParseFactorySyntaxSource(stackPath, src)
	if err != nil {
		return err
	}

	refs, errs := resolve.ExtractSyntaxBodyImports(sf.Factory.Body)
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	projectDir, err := printGraphProjectDir(filepath.Dir(stackPath))
	if err != nil {
		return err
	}
	project, err := printGraphProject(projectDir)
	if err != nil {
		return err
	}
	var replaceMap map[deps.Dependency]string
	if project != nil {
		if err := deps.CheckReplacementSentinels(project); err != nil {
			return err
		}
		replaceMap = project.Replace
	}

	projectLock, err := printGraphProjectLock(projectDir)
	if err != nil {
		return err
	}
	resolver, err := newCompileResolver(projectDir)
	if err != nil {
		return err
	}
	resolver = compile.WrapProjectLockSources(resolver, projectLock)
	resolver, err = compile.WrapReplaces(resolver, projectDir, cfg.replaceUnobin, replaceMap)
	if err != nil {
		return err
	}

	repoVersions, err := compile.ProjectLockVersions(projectDir)
	if err != nil {
		return err
	}
	repoVersions = printGraphReplacedVersions(
		repoVersions, cfg.replaceUnobin != "", replaceMap)
	replaceUnobin, err := printGraphUnobinReplace(projectDir, cfg.replaceUnobin, replaceMap)
	if err != nil {
		return err
	}
	schemaRoots := compile.UnobinSchemaRoots(
		cmd.ErrOrStderr(), replaceUnobin, cliVersion())
	analysis, err := sourcecheck.AnalyzeImports(refs, sourcecheck.ImportAnalysisOptions{
		Resolver:    resolver,
		Versions:    repoVersions,
		WarnOut:     cmd.ErrOrStderr(),
		SchemaCache: compile.NewSchemaCache(schemaRoots...),
		Source: &resolve.Source{
			FS:   os.DirFS(filepath.Dir(stackPath)),
			Path: filepath.Dir(stackPath),
		},
	})
	if err != nil {
		return err
	}
	libs := analysis.Libraries
	checker := check.NewSyntax(sf.Factory.Body, libs)
	if errs := checker.References(nil); errs.Len() > 0 {
		return errs.Err()
	}

	dag := checker.DAG()
	out := cmd.OutOrStdout()
	switch cfg.format {
	case "plain":
		graphprint.Plain(out, dag)
	case "dot":
		graphprint.DOT(out, dag, compile.DeriveStackName(stackPath))
	default:
		return fmt.Errorf("unknown --format %q (want 'plain' or 'dot')", cfg.format)
	}
	return nil
}

func printGraphProjectDir(sourceDir string) (string, error) {
	projectDir, err := deps.FindProjectDir(sourceDir)
	if err == nil {
		return projectDir, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return sourceDir, nil
	}
	return "", err
}

func printGraphProjectLock(projectDir string) (*deps.ProjectLock, error) {
	projectLock, err := deps.ReadProjectLock(os.DirFS(projectDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return projectLock, nil
}

func printGraphProject(projectDir string) (*deps.Project, error) {
	project, err := deps.ReadProject(os.DirFS(projectDir))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return project, nil
}

func printGraphReplacedVersions(
	versions map[string]string,
	replaceUnobin bool,
	replace map[deps.Dependency]string,
) map[string]string {
	if !replaceUnobin && len(replace) == 0 {
		return versions
	}
	if versions == nil {
		versions = map[string]string{}
	}
	if replaceUnobin {
		versions[toolchain.UnobinModulePath] = deps.ReplacementSentinel
	}
	for dep := range replace {
		if dep.Subdir == "" {
			versions[dep.URL] = deps.ReplacementSentinel
		} else {
			versions[dep.String()] = deps.ReplacementSentinel
		}
	}
	return versions
}

func printGraphUnobinReplace(
	projectDir string,
	cliReplace string,
	replace map[deps.Dependency]string,
) (string, error) {
	if cliReplace != "" {
		return filepath.Abs(cliReplace)
	}
	path, ok := replace[deps.Dependency{URL: toolchain.UnobinModulePath}]
	if !ok {
		return "", nil
	}
	return printGraphAbsReplacePath(projectDir, path)
}

func printGraphAbsReplacePath(root, path string) (string, error) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	return filepath.Abs(path)
}
