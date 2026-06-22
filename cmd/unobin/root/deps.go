package root

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/compile"
	"github.com/cloudboss/unobin/pkg/deps"
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsList(cmd, depsListCfg)
		},
	}

	depsVerifyCfg = &depsSyncConfig{}
	depsVerifyCmd = &cobra.Command{
		Use:   "verify",
		Short: "Check the cached dependencies against project-lock",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDepsVerify(cmd, depsVerifyCfg)
		},
	}

	depsCleanCmd = &cobra.Command{
		Use:   "clean",
		Short: "Remove the cached dependency sources",
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

const (
	depsPathHelp    = "Path to the factory source file or project directory."
	depsReplaceHelp = "Local path to substitute for github.com/cloudboss/unobin so the " +
		"resolver reads from a working tree instead of fetching."
)

func init() {
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
	root, err := projectRoot(cfg.stackPath)
	if err != nil {
		return err
	}
	project, projectName, err := readProjectOrEmpty(root)
	if err != nil {
		return err
	}
	imported, err := deps.ImportedPackages(root)
	if err != nil {
		return err
	}
	projectLock, err := readProjectLockOrNil(root)
	if err != nil {
		return err
	}
	resolver, err := newDepsResolver(root, cfg.replaceUnobin, project.Replace)
	if err != nil {
		return err
	}
	if err := reconcileProject(projectName, project, imported, projectLock, resolver); err != nil {
		return err
	}
	return resolveAndWrite(cmd, root, project, cfg.replaceUnobin)
}

// runDepsGet resolves a version for one dependency, sets its floor in the
// project, and re-pins. The query may be empty or "latest" (the highest
// tag), an exact version, or a partial one (v1, v1.2).
func runDepsGet(cmd *cobra.Command, cfg *depsSyncConfig, arg string) error {
	root, err := projectRoot(cfg.stackPath)
	if err != nil {
		return err
	}
	dep, query, err := parseGetArg(arg)
	if err != nil {
		return err
	}
	if deps.IsReplacementSentinel(query) {
		return fmt.Errorf("%s is reserved for project replacements", query)
	}
	if dep.URL == toolchain.UnobinModulePath {
		return fmt.Errorf(
			"%s is toolchain-versioned; pin it with the project's unobin-version line",
			dep.URL)
	}
	tags, err := depsListTags(dep.URL)
	if err != nil {
		return err
	}
	version, err := deps.ResolveVersion(dep, query, tags)
	if err != nil {
		return err
	}
	project, projectName, err := readProjectOrEmpty(root)
	if err != nil {
		return err
	}
	resolver, err := newDepsResolver(root, cfg.replaceUnobin, project.Replace)
	if err != nil {
		return err
	}
	if err := deps.RequireProject(dep, version, resolver); err != nil {
		return err
	}
	imported, err := deps.ImportedPackages(root)
	if err != nil {
		return err
	}
	targetIsDirect := dependencyOwnsImportedPackage(dep, imported)
	if targetIsDirect {
		project.SetRequire(dep, version, false)
	}
	projectLock, err := readProjectLockOrNil(root)
	if err != nil {
		return err
	}
	direct, err := directRequirementsForImports(projectName, project, imported, projectLock, resolver)
	if err != nil {
		return err
	}
	for directDep, directVersion := range direct {
		project.SetRequire(directDep, directVersion, false)
	}
	if !targetIsDirect {
		reachable, err := reachableRequirements(direct, project.Replace, resolver)
		if err != nil {
			return err
		}
		if !reachable[dep] {
			return fmt.Errorf(
				"%s is not imported directly or transitively by this project", dep)
		}
		project.SetRequire(dep, version, true)
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Using %s %s\n", dep, version)
	return resolveAndWrite(cmd, root, project, cfg.replaceUnobin)
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
	cmd *cobra.Command, root string, project *deps.Project, replaceUnobin string,
) error {
	if err := deps.CheckReplacementSentinels(project); err != nil {
		return err
	}
	resolver, err := newDepsResolver(root, replaceUnobin, project.Replace)
	if err != nil {
		return err
	}
	selection, err := deps.Resolve(project, deps.NewFetcher(resolver))
	if err != nil {
		return err
	}
	projectLock, err := deps.ProjectLockFromImports(os.DirFS(root), selection, resolver, project.Replace)
	if err != nil {
		return err
	}
	projectName, err := writeProject(root, project)
	if err != nil {
		return err
	}
	projectLock.ToolchainVersion = cliVersion()
	if err := deps.WriteProjectLock(filepath.Join(root, deps.ProjectLockFileName), projectLock); err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(),
		"Wrote %s (%d direct, %d indirect) and %s (%d selected)\n",
		projectName, project.DirectCount(), project.IndirectCount(),
		deps.ProjectLockFileName, len(projectLock.Deps))
	return nil
}

func writeProject(root string, project *deps.Project) (string, error) {
	path := filepath.Join(root, deps.ProjectFileName)
	return deps.ProjectFileName, deps.WriteProject(path, project)
}

// runDepsList prints the project-lock dependencies, one per line, sorted by id.
func runDepsList(cmd *cobra.Command, cfg *depsSyncConfig) error {
	projectLock, err := readProjectLock(cfg.stackPath)
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	for _, id := range projectLock.SortedIDs() {
		d := projectLock.Deps[id]
		fmt.Fprintf(out, "%s %s (%s)\n", id, d.Version, d.Kind)
	}
	return nil
}

// runDepsVerify re-fetches the project-lock UB dependencies and reports any
// whose content no longer matches the recorded hash.
func runDepsVerify(cmd *cobra.Command, cfg *depsSyncConfig) error {
	projectLock, err := readProjectLock(cfg.stackPath)
	if err != nil {
		return err
	}
	root, err := projectRoot(cfg.stackPath)
	if err != nil {
		return err
	}
	resolver, err := newDepsResolver(root, cfg.replaceUnobin, nil)
	if err != nil {
		return err
	}
	mismatches, err := deps.Verify(projectLock, resolver)
	if err != nil {
		return err
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("verification failed:\n  %s", strings.Join(mismatches, "\n  "))
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
	resolver, err := newRemoteResolver()
	if err != nil {
		return err
	}
	dir, err := resolver.CleanImports()
	if err != nil {
		return err
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
