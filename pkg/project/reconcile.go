package project

import (
	"fmt"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/deps"
	"github.com/cloudboss/unobin/pkg/resolve"
	"github.com/cloudboss/unobin/pkg/toolchain"
)

func reconcileProject(
	projectName string,
	m *deps.Project,
	imported map[deps.RemotePackage]bool,
	projectLock *deps.ProjectLock,
	resolver resolve.Resolver,
	listTags func(string) ([]string, error),
) error {
	direct, err := directRequirementsForImports(
		projectName, m, imported, projectLock, resolver, listTags)
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
	listTags func(string) ([]string, error),
) (map[deps.Dependency]string, error) {
	projects := deps.ProjectIDsFromDependencies(m.Requires)
	projectLockProjects := projectLockProjectIDs(projectLock)
	replaced := deps.ProjectIDsFromReplace(m.Replace)
	direct := map[deps.Dependency]string{}
	var missing []string
	packages := make([]deps.RemotePackage, 0, len(imported))
	for pkg := range imported {
		packages = append(packages, pkg)
	}
	slices.SortFunc(packages, func(a, b deps.RemotePackage) int {
		return strings.Compare(a.String(), b.String())
	})
	for _, pkg := range packages {
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
		discovered, version, found, err := discoverImportOwner(pkg, resolver, listTags)
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
	listTags func(string) ([]string, error),
) (deps.PackageOwner, string, bool, error) {
	tags, err := listTags(pkg.URL)
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
