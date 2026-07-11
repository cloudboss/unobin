package root

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/internal/cmdout"
	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/diagnostic"
	"github.com/cloudboss/unobin/pkg/filechange"
	"github.com/cloudboss/unobin/pkg/git"
	"github.com/cloudboss/unobin/pkg/projectmarker"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/toolchain"
	"github.com/spf13/cobra"
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

// depsListTags lists a repository's tags. It is a package var so tests can
// resolve versions without a network round trip.
var depsListTags = func(url string) ([]string, error) {
	return git.ListTags(context.Background(), resolve.WithDefaultScheme(url))
}

var newRemoteResolver = resolve.NewRemoteResolver

// SetDepsListTagsForTest replaces tag listing and returns a restore function.
func SetDepsListTagsForTest(listTags func(string) ([]string, error)) func() {
	prev := depsListTags
	depsListTags = listTags
	return func() { depsListTags = prev }
}

// SetRemoteResolverForTest replaces remote resolver construction and returns
// a restore function.
func SetRemoteResolverForTest(newResolver func() (*resolve.RemoteResolver, error)) func() {
	prev := newRemoteResolver
	newRemoteResolver = newResolver
	return func() { newRemoteResolver = prev }
}

type depsSyncConfig struct {
	stackPath     string
	replaceUnobin string
}

type dependencyListEntry struct {
	ID       string               `json:"id"       ub:"id"`
	Kind     deps.ProjectLockKind `json:"kind"     ub:"kind"`
	Version  string               `json:"version"  ub:"version"`
	Indirect bool                 `json:"indirect" ub:"indirect"`
}

type dependencyListResult struct {
	Kind          string                  `json:"kind"           ub:"kind"`
	FormatVersion int                     `json:"format-version" ub:"format-version"`
	Dependencies  []dependencyListEntry   `json:"dependencies"   ub:"dependencies"`
	Diagnostics   []diagnostic.Diagnostic `json:"diagnostics"    ub:"diagnostics"`
}

type dependencyWriteResult struct {
	ProjectFile string
	LockFile    string
	Direct      int
	Indirect    int
	Selected    int
	Files       []filechange.Change
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
}

type dependencyGetOperation struct {
	Dependency string
	Version    string
	Indirect   bool
	Write      *dependencyWriteResult
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
	result *dependencyWriteResult,
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

func dependencyDiagnostics() []diagnostic.Diagnostic {
	return []diagnostic.Diagnostic{}
}

// projectRoot resolves the project root from a --path value. When an
// ancestor has project.ub, that directory is the project root. Without a
// project, the path itself is the root when it is a directory; otherwise its
// parent is used so first-time deps sync can create project.ub there.
func projectRoot(stackPath string) (string, error) {
	root, marker, err := deps.FindProjectMarkerDir(stackPath)
	if err == nil {
		if marker.Kind == projectmarker.Go {
			return "", fmt.Errorf("deps sync manages UB projects; use Go commands for Go modules")
		}
		return root, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", err
	}
	if info, err := os.Stat(stackPath); err == nil && info.IsDir() {
		return stackPath, nil
	}
	return filepath.Dir(stackPath), nil
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
	result, err := syncDependencies(cfg, dependencyToolOutput(cmd, format))
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
			Diagnostics:   dependencyDiagnostics(),
		})
	}
	fmt.Fprintf(cmd.ErrOrStderr(),
		"Wrote %s (%d direct, %d indirect) and %s (%d selected)\n",
		result.ProjectFile, result.Direct, result.Indirect,
		result.LockFile, result.Selected)
	return nil
}

func syncDependencies(
	cfg *depsSyncConfig,
	toolOutput io.Writer,
) (*dependencyWriteResult, error) {
	root, err := projectRoot(cfg.stackPath)
	if err != nil {
		return nil, err
	}
	project, projectName, err := readProjectOrEmpty(root)
	if err != nil {
		return nil, err
	}
	imported, err := deps.ImportedPackages(root)
	if err != nil {
		return nil, err
	}
	projectLock, err := readProjectLockOrNil(root)
	if err != nil {
		return nil, err
	}
	resolver, err := newDepsResolver(root, cfg.replaceUnobin, project.Replace)
	if err != nil {
		return nil, err
	}
	if err := reconcileProject(projectName, project, imported, projectLock, resolver); err != nil {
		return nil, err
	}
	return resolveAndWrite(root, project, cfg.replaceUnobin, toolOutput)
}

// runDepsGet resolves a version for one dependency, sets its floor in the
// project, and re-pins. The query may be empty or "latest" (the highest
// tag), an exact version, or a partial one (v1, v1.2).
func runDepsGet(cmd *cobra.Command, cfg *depsSyncConfig, arg string) error {
	format, err := dependencyCommandFormat(cmd)
	if err != nil {
		return err
	}
	var announce func(deps.Dependency, string)
	if format == cmdout.FormatText {
		announce = func(dependency deps.Dependency, version string) {
			fmt.Fprintf(cmd.ErrOrStderr(), "Using %s %s\n", dependency, version)
		}
	}
	operation, err := getDependency(
		cfg, arg, dependencyToolOutput(cmd, format), announce,
	)
	if err != nil {
		var result *dependencyWriteResult
		if operation != nil {
			result = operation.Write
		}
		return dependencyCommandFailure(cmd, format, result, err)
	}
	if format.Machine() {
		result := operation.Write
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, dependencyGetResult{
			Kind:          "dependency-get-result",
			FormatVersion: 1,
			Dependency:    operation.Dependency,
			Version:       operation.Version,
			Indirect:      operation.Indirect,
			ProjectFile:   result.ProjectFile,
			LockFile:      result.LockFile,
			Direct:        result.Direct,
			Selected:      result.Selected,
			Files:         result.Files,
			Diagnostics:   dependencyDiagnostics(),
		})
	}
	result := operation.Write
	fmt.Fprintf(cmd.ErrOrStderr(),
		"Wrote %s (%d direct, %d indirect) and %s (%d selected)\n",
		result.ProjectFile, result.Direct, result.Indirect,
		result.LockFile, result.Selected)
	return nil
}

func getDependency(
	cfg *depsSyncConfig,
	arg string,
	toolOutput io.Writer,
	announce func(deps.Dependency, string),
) (*dependencyGetOperation, error) {
	root, err := projectRoot(cfg.stackPath)
	if err != nil {
		return nil, err
	}
	dep, query, err := parseGetArg(arg)
	if err != nil {
		return nil, err
	}
	if deps.IsReplacementSentinel(query) {
		return nil, fmt.Errorf("%s is reserved for project replacements", query)
	}
	if dep.URL == toolchain.UnobinModulePath {
		return nil, fmt.Errorf(
			"%s is toolchain-versioned; pin it with the project's unobin-version line",
			dep.URL)
	}
	tags, err := depsListTags(dep.URL)
	if err != nil {
		return nil, err
	}
	version, err := deps.ResolveVersion(dep, query, tags)
	if err != nil {
		return nil, err
	}
	project, projectName, err := readProjectOrEmpty(root)
	if err != nil {
		return nil, err
	}
	resolver, err := newDepsResolver(root, cfg.replaceUnobin, project.Replace)
	if err != nil {
		return nil, err
	}
	if err := deps.RequireProject(dep, version, resolver); err != nil {
		return nil, err
	}
	imported, err := deps.ImportedPackages(root)
	if err != nil {
		return nil, err
	}
	targetIsDirect := dependencyOwnsImportedPackage(dep, imported)
	if targetIsDirect {
		project.SetRequire(dep, version, false)
	}
	projectLock, err := readProjectLockOrNil(root)
	if err != nil {
		return nil, err
	}
	direct, err := directRequirementsForImports(projectName, project, imported, projectLock, resolver)
	if err != nil {
		return nil, err
	}
	for directDep, directVersion := range direct {
		project.SetRequire(directDep, directVersion, false)
	}
	if !targetIsDirect {
		reachable, err := reachableRequirements(direct, project.Replace, resolver)
		if err != nil {
			return nil, err
		}
		if !reachable[dep] {
			return nil, fmt.Errorf(
				"%s is not imported directly or transitively by this project", dep)
		}
		project.SetRequire(dep, version, true)
	}
	if announce != nil {
		announce(dep, version)
	}
	writeResult, err := resolveAndWrite(root, project, cfg.replaceUnobin, toolOutput)
	return &dependencyGetOperation{
		Dependency: dep.String(),
		Version:    version,
		Indirect:   !targetIsDirect,
		Write:      writeResult,
	}, err
}

// readProjectOrEmpty reads the project file from root, returning an
// empty project when the file does not exist yet. There is no `deps init`:
// the project is created the first time get or sync writes it.
func readProjectOrEmpty(root string) (*deps.Project, string, error) {
	project, err := deps.ReadProject(os.DirFS(root))
	if errors.Is(err, fs.ErrNotExist) {
		return &deps.Project{
			Requires: map[deps.Dependency]deps.Requirement{},
		}, deps.ProjectFileName, nil
	}
	if err != nil {
		return nil, deps.ProjectFileName, err
	}
	return project, deps.ProjectFileName, nil
}

// reconcileProject makes the project's project floors match the imported
// remote packages. An imported package with no owning project floor is an error
// that points the author at `deps get`; a floor whose project owns no import is
// kept only when the direct dependency graph reaches it. The unobin repository
// takes no floor at all: an import from it must be served by a replace, since
// its source version may not float free of the toolchain.
func reconcileProject(
	projectName string,
	m *deps.Project,
	imported map[deps.RemotePackage]bool,
	projectLock *deps.ProjectLock,
	resolver resolve.Resolver,
) error {
	direct, err := directRequirementsForImports(projectName, m, imported, projectLock, resolver)
	if err != nil {
		return err
	}
	reachable, err := reachableRequirements(direct, m.Replace, resolver)
	if err != nil {
		return err
	}
	next := map[deps.Dependency]deps.Requirement{}
	for dep, version := range direct {
		next[dep] = deps.Requirement{Version: version}
	}
	for dep, req := range m.Requires {
		if _, ok := direct[dep]; ok {
			continue
		}
		if reachable[dep] {
			next[dep] = deps.Requirement{Version: req.Version, Indirect: true}
		}
	}
	m.Requires = next
	return nil
}

func directRequirementsForImports(
	projectName string,
	m *deps.Project,
	imported map[deps.RemotePackage]bool,
	projectLock *deps.ProjectLock,
	resolver resolve.Resolver,
) (map[deps.Dependency]string, error) {
	projects := deps.ProjectIDsFromDependencies(m.Requires)
	projectLockProjects := projectLockProjectIDs(projectLock)
	replaced := deps.ProjectIDsFromReplace(m.Replace)
	direct := map[deps.Dependency]string{}
	var missing []string
	for pkg := range imported {
		replacement, hasReplacement := deps.MostSpecificProject(replaced, pkg)
		if pkg.URL == toolchain.UnobinModulePath {
			if !hasReplacement {
				return nil, fmt.Errorf(
					"%s is toolchain-versioned and cannot be imported at a dependency"+
						" version; replace it locally:\n"+
						"  in project.ub: project: { replace: { '%s': '<path-to-unobin>' } }",
					pkg.URL, pkg.URL)
			}
			continue
		}
		owner, ok := deps.MostSpecificProject(projects, pkg)
		if ok {
			dep := owner.Project.Dependency()
			direct[dep] = m.Requires[dep].Version
			continue
		}
		owner, ok = deps.MostSpecificProject(projectLockProjects, pkg)
		if ok {
			dep := owner.Project.Dependency()
			direct[dep] = projectLock.Deps[owner.Project.String()].Version
			projects = append(projects, owner.Project)
			continue
		}
		if hasReplacement {
			dep := replacement.Project.Dependency()
			direct[dep] = deps.ReplacementSentinel
			projects = append(projects, replacement.Project)
			continue
		}
		discovered, version, found, err := discoverImportOwner(pkg, resolver)
		if err != nil {
			return nil, err
		}
		if found {
			dep := discovered.Project.Dependency()
			direct[dep] = version
			projects = append(projects, discovered.Project)
			continue
		}
		missing = append(missing, pkg.String())
	}
	if len(missing) > 0 {
		slices.Sort(missing)
		return nil, fmt.Errorf(
			"imported but missing an owning project in %s: %s\n"+
				"add the owning project with `unobin deps get <project>@<version>`",
			projectName, strings.Join(missing, ", "))
	}
	return direct, nil
}

func reachableRequirements(
	direct map[deps.Dependency]string,
	replace map[deps.Dependency]string,
	resolver resolve.Resolver,
) (map[deps.Dependency]bool, error) {
	project := &deps.Project{
		Requires: map[deps.Dependency]deps.Requirement{},
		Replace:  replace,
	}
	for dep, version := range direct {
		project.SetRequire(dep, version, false)
	}
	selection, err := deps.Resolve(project, deps.NewFetcher(resolver))
	if err != nil {
		return nil, err
	}
	reachable := map[deps.Dependency]bool{}
	for dep := range selection {
		reachable[dep] = true
	}
	return reachable, nil
}

func dependencyOwnsImportedPackage(
	dep deps.Dependency,
	imported map[deps.RemotePackage]bool,
) bool {
	project := deps.ProjectIDFromDependency(dep)
	for pkg := range imported {
		if _, ok := deps.ProjectContains(project, pkg); ok {
			return true
		}
	}
	return false
}

func discoverImportOwner(
	pkg deps.RemotePackage, resolver resolve.Resolver,
) (deps.PackageOwner, string, bool, error) {
	tags, err := depsListTags(pkg.URL)
	if err != nil {
		return deps.PackageOwner{}, "", false, err
	}
	for _, project := range importOwnerCandidates(pkg) {
		dep := project.Dependency()
		versions := deps.Versions(dep, tags)
		if len(versions) == 0 {
			continue
		}
		version := versions[len(versions)-1]
		owner, ok := deps.ProjectContains(project, pkg)
		if !ok {
			continue
		}
		found, err := discoveredProjectHasMarker(project, version, resolver)
		if err != nil {
			return deps.PackageOwner{}, "", false, err
		}
		if !found {
			continue
		}
		packageOwner := deps.PackageOwner{Project: project, PackageSubdir: owner}
		blocked, err := blockedByNestedProject(packageOwner, pkg, resolver, version)
		if err != nil {
			return deps.PackageOwner{}, "", false, err
		}
		if blocked {
			continue
		}
		return packageOwner, version, true, nil
	}
	return deps.PackageOwner{}, "", false, nil
}

func importOwnerCandidates(pkg deps.RemotePackage) []deps.ProjectID {
	var candidates []deps.ProjectID
	for subdir := pkg.Subdir; ; subdir = parentSubdir(subdir) {
		candidates = append(candidates, deps.ProjectID{URL: pkg.URL, Subdir: subdir})
		if subdir == "" {
			break
		}
	}
	return candidates
}

func parentSubdir(subdir string) string {
	if subdir == "" {
		return ""
	}
	if i := strings.LastIndex(subdir, "/"); i >= 0 {
		return subdir[:i]
	}
	return ""
}

func discoveredProjectHasMarker(
	project deps.ProjectID, version string, resolver resolve.Resolver,
) (bool, error) {
	src, err := resolver.Resolve(&resolve.RemoteImport{
		URL:           project.URL,
		Subdir:        project.Subdir,
		ProjectSubdir: project.Subdir,
		PackageSubdir: project.Subdir,
		Version:       deps.ProjectTag(project, version),
	})
	if err != nil {
		return false, err
	}
	return deps.HasProjectMarker(src.FS)
}

func blockedByNestedProject(
	owner deps.PackageOwner,
	pkg deps.RemotePackage,
	resolver resolve.Resolver,
	version string,
) (bool, error) {
	src, err := resolver.Resolve(&resolve.RemoteImport{
		URL:           pkg.URL,
		Subdir:        pkg.Subdir,
		ProjectSubdir: owner.Project.Subdir,
		PackageSubdir: pkg.Subdir,
		Version:       deps.ProjectTag(owner.Project, version),
	})
	if err != nil {
		return false, nil
	}
	if err := deps.CheckPackageBoundary(src, owner, pkg); err != nil {
		if strings.Contains(err.Error(), "does not own package") {
			return true, nil
		}
		return false, err
	}
	return false, nil
}

func projectLockProjectIDs(projectLock *deps.ProjectLock) []deps.ProjectID {
	if projectLock == nil {
		return nil
	}
	projects := make([]deps.ProjectID, 0, len(projectLock.Deps))
	for id := range projectLock.Deps {
		dep, err := deps.ParseDependency(id)
		if err != nil {
			continue
		}
		projects = append(projects, deps.ProjectIDFromDependency(dep))
	}
	return projects
}

func parseGetArg(arg string) (deps.Dependency, string, error) {
	repoPart, query := arg, ""
	if at := strings.LastIndex(arg, "@"); at >= 0 {
		repoPart, query = arg[:at], arg[at+1:]
	}
	dep, err := deps.ParseDependency(repoPart)
	return dep, query, err
}

// resolveAndWrite selects versions across project's dependency graph,
// walks the imports to build project-lock, and writes both files at root.
func resolveAndWrite(
	root string,
	project *deps.Project,
	replaceUnobin string,
	toolOutput io.Writer,
) (*dependencyWriteResult, error) {
	if err := deps.CheckReplacementSentinels(project); err != nil {
		return nil, err
	}
	resolver, err := newDepsResolver(root, replaceUnobin, project.Replace)
	if err != nil {
		return nil, err
	}
	selection, err := deps.Resolve(project, deps.NewFetcher(resolver))
	if err != nil {
		return nil, err
	}
	unobinReplace, err := printGraphUnobinReplace(root, replaceUnobin, project.Replace)
	if err != nil {
		return nil, err
	}
	schemaRoots := compile.UnobinSchemaRoots(toolOutput, unobinReplace, cliVersion())
	projectLock, err := deps.ProjectLockFromImportsWithSchemaRoots(
		os.DirFS(root), selection, resolver, project.Replace, schemaRoots)
	if err != nil {
		return nil, err
	}
	projectLock.ToolchainVersion = cliVersion()
	return writeDependencyFiles(root, project, projectLock)
}

func writeDependencyFiles(
	root string,
	project *deps.Project,
	projectLock *deps.ProjectLock,
) (*dependencyWriteResult, error) {
	result := &dependencyWriteResult{
		ProjectFile: deps.ProjectFileName,
		LockFile:    deps.ProjectLockFileName,
		Direct:      project.DirectCount(),
		Indirect:    project.IndirectCount(),
		Selected:    len(projectLock.Deps),
		Files:       []filechange.Change{},
	}
	projectPath := filepath.Join(root, deps.ProjectFileName)
	projectChange, err := deps.WriteProjectChange(projectPath, project)
	if err != nil {
		return nil, err
	}
	projectChange.Path = deps.ProjectFileName
	result.Files = append(result.Files, projectChange)
	lockPath := filepath.Join(root, deps.ProjectLockFileName)
	lockChange, err := deps.WriteProjectLockChange(lockPath, projectLock)
	if err != nil {
		return result, err
	}
	lockChange.Path = deps.ProjectLockFileName
	result.Files = append(result.Files, lockChange)
	result.Files, err = filechange.Compose(result.Files)
	if err != nil {
		return result, err
	}
	return result, nil
}

// runDepsList prints the project-lock dependencies, one per line, sorted by id.
func runDepsList(cmd *cobra.Command, cfg *depsSyncConfig) error {
	format, err := dependencyCommandFormat(cmd)
	if err != nil {
		return err
	}
	root, err := projectRoot(cfg.stackPath)
	if err != nil {
		return dependencyCommandFailure(cmd, format, nil, err)
	}
	projectLock, err := readProjectLock(cfg.stackPath)
	if err != nil {
		return dependencyCommandFailure(cmd, format, nil, err)
	}
	project, err := deps.ReadProject(os.DirFS(root))
	if errors.Is(err, fs.ErrNotExist) {
		project = nil
	} else if err != nil {
		return dependencyCommandFailure(cmd, format, nil, err)
	}
	result := dependencyListResult{
		Kind:          "dependency-list",
		FormatVersion: 1,
		Dependencies:  []dependencyListEntry{},
		Diagnostics:   dependencyDiagnostics(),
	}
	for _, id := range projectLock.SortedIDs() {
		selected := projectLock.Deps[id]
		indirect := true
		if project != nil {
			dependency, parseErr := deps.ParseDependency(id)
			if parseErr != nil {
				return dependencyCommandFailure(cmd, format, nil, parseErr)
			}
			if requirement, ok := project.Requires[dependency]; ok {
				indirect = requirement.Indirect
			}
		}
		result.Dependencies = append(result.Dependencies, dependencyListEntry{
			ID:       id,
			Kind:     selected.Kind,
			Version:  selected.Version,
			Indirect: indirect,
		})
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
	projectLock, err := readProjectLock(cfg.stackPath)
	if err != nil {
		return dependencyCommandFailure(cmd, format, nil, err)
	}
	root, err := projectRoot(cfg.stackPath)
	if err != nil {
		return dependencyCommandFailure(cmd, format, nil, err)
	}
	resolver, err := newDepsResolver(root, cfg.replaceUnobin, nil)
	if err != nil {
		return dependencyCommandFailure(cmd, format, nil, err)
	}
	verified, err := deps.Verify(projectLock, resolver)
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

// readProjectLock reads project-lock from stackPath's project root, with a
// clear error when it is missing.
func readProjectLock(stackPath string) (*deps.ProjectLock, error) {
	root, rootErr := projectRoot(stackPath)
	if rootErr != nil {
		return nil, rootErr
	}
	projectLock, err := deps.ReadProjectLock(os.DirFS(root))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("no %s found; run `unobin deps sync` first",
				deps.ProjectLockFileName)
		}
		return nil, err
	}
	return projectLock, nil
}

func readProjectLockOrNil(root string) (*deps.ProjectLock, error) {
	projectLock, err := deps.ReadProjectLock(os.DirFS(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return projectLock, nil
}

// runDepsClean removes the cached dependency sources, which are shared
// across projects.
func runDepsClean(cmd *cobra.Command) error {
	format, err := dependencyCommandFormat(cmd)
	if err != nil {
		return err
	}
	resolver, err := newRemoteResolver()
	if err != nil {
		return dependencyCommandFailure(cmd, format, nil, err)
	}
	removed := false
	if _, err := os.Stat(resolver.ImportsDir()); err == nil {
		removed = true
	} else if !errors.Is(err, fs.ErrNotExist) {
		return dependencyCommandFailure(cmd, format, nil, err)
	}
	dir, err := resolver.CleanImports()
	if err != nil {
		return dependencyCommandFailure(cmd, format, nil, err)
	}
	if format.Machine() {
		return cmdout.WriteDocument(cmd.OutOrStdout(), format, dependencyCacheCleanResult{
			Kind:          "dependency-cache-clean-result",
			FormatVersion: 1,
			Removed:       removed,
			Diagnostics:   dependencyDiagnostics(),
		})
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Removed the import cache at %s\n", dir)
	return nil
}

func newDepsResolver(
	root, replaceUnobin string, replace map[deps.Dependency]string,
) (resolve.Resolver, error) {
	resolver, err := newCompileResolver(root)
	if err != nil {
		return nil, err
	}
	return compile.WrapReplaces(resolver, root, replaceUnobin, replace)
}
