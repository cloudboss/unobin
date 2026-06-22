package root

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/cloudboss/unobin/pkg/check"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/goschema"
	"github.com/cloudboss/unobin/pkg/graphprint"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/runtime"
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
	libs, err := buildLibraryMap(
		refs,
		resolver,
		repoVersions,
		cmd.ErrOrStderr(),
		schemaRoots,
		&resolve.Source{FS: os.DirFS(filepath.Dir(stackPath)), Path: filepath.Dir(stackPath)},
	)
	if err != nil {
		return err
	}
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

// buildLibraryMap turns each top-level import alias into a *runtime.Library.
// UB-library Composites are populated from the library's source-declared
// body files; Go libraries become empty Library values so the runtime can
// tell "imported but not a composite" apart from "not imported at all". Each
// composite carries its own Libraries map so composite-internal lookups stay
// self-contained.
func buildLibraryMap(
	refs map[string]resolve.ImportRef,
	resolver resolve.Resolver,
	versions map[string]string,
	warnOut io.Writer,
	schemaRoots []goschema.ModuleRoot,
	source *resolve.Source,
) (map[string]*runtime.Library, error) {
	schemas := compile.NewSchemaCache(schemaRoots...)
	v := &graphVisitor{
		byKey:   map[string]*runtime.Library{},
		warnOut: warnOut,
		schemas: schemas,
	}
	top, err := resolve.WalkUBFrom(refs, resolver, v, versions, source)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*runtime.Library, len(top))
	for _, res := range top {
		switch res.Kind {
		case resolve.ResolutionGo:
			schema, warnings, err := schemas.Read(res.SourcePath)
			if err != nil {
				return nil, fmt.Errorf("import %q: %w", res.LocalAlias, err)
			}
			compile.PrintSchemaWarnings(warnOut, res.LocalAlias, warnings)
			out[res.LocalAlias] = &runtime.Library{Schema: schema}
		case resolve.ResolutionUB:
			out[res.LocalAlias] = v.byKey[res.CanonicalKey]
		}
	}
	return out, nil
}

// graphVisitor builds a *runtime.Library per unique UB-library key.
// Go imports contribute nothing to its state because print-graph
// doesn't model their types; the consumer fills in an empty
// *runtime.Library per top-level Go alias.
type graphVisitor struct {
	byKey   map[string]*runtime.Library
	warnOut io.Writer
	schemas *compile.SchemaCache
}

func (g *graphVisitor) OnGoImport(_, _, _, _ string) error {
	return nil
}

func (g *graphVisitor) OnUBLibrary(
	_, canonicalKey string, _ resolve.ImportRef, lib *resolve.UBLibrary,
) error {
	runtimeLib := &runtime.Library{}
	for _, entry := range lib.CompositeEntries() {
		resols := lib.BodyImports[entry.Kind][entry.Name]
		bodyLibs := make(map[string]*runtime.Library, len(resols))
		for _, res := range resols {
			switch res.Kind {
			case resolve.ResolutionGo:
				schema, warnings, err := g.schemas.Read(res.SourcePath)
				if err != nil {
					return fmt.Errorf(
						"%s composite %q import %q: %w",
						entry.Kind, entry.Name, res.LocalAlias, err)
				}
				compile.PrintSchemaWarnings(g.warnOut, res.LocalAlias, warnings)
				bodyLibs[res.LocalAlias] = &runtime.Library{Schema: schema}
			case resolve.ResolutionUB:
				bodyLibs[res.LocalAlias] = g.byKey[res.CanonicalKey]
			}
		}
		syntaxBody := entry.SyntaxBody
		composite := &runtime.CompositeType{
			Name:       entry.Name,
			Kind:       runtime.NodeKind(entry.Kind),
			SyntaxBody: &syntaxBody,
			Libraries:  bodyLibs,
		}
		runtimeLib.AddComposite(composite)
	}
	g.byKey[canonicalKey] = runtimeLib
	return nil
}
