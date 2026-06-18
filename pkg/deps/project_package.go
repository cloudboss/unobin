package deps

import "strings"

// ProjectID identifies a versioned dependency project.
type ProjectID struct {
	URL    string
	Subdir string
}

// RemotePackage identifies a package import below a dependency project.
type RemotePackage struct {
	URL    string
	Subdir string
}

// PackageOwner records the project that owns a package import.
type PackageOwner struct {
	Project       ProjectID
	PackageSubdir string
}

func (p ProjectID) Dependency() Dependency {
	return Dependency{URL: p.URL, Subdir: p.Subdir}
}

func (p ProjectID) String() string {
	return p.Dependency().String()
}

func (p RemotePackage) Dependency() Dependency {
	return Dependency{URL: p.URL, Subdir: p.Subdir}
}

func (p RemotePackage) String() string {
	return p.Dependency().String()
}

func ProjectIDFromDependency(dep Dependency) ProjectID {
	return ProjectID{URL: dep.URL, Subdir: dep.Subdir}
}

func RemotePackageFromDependency(dep Dependency) RemotePackage {
	return RemotePackage{URL: dep.URL, Subdir: dep.Subdir}
}

func ProjectIDsFromDependencies(deps map[Dependency]string) []ProjectID {
	projects := make([]ProjectID, 0, len(deps))
	for dep := range deps {
		projects = append(projects, ProjectIDFromDependency(dep))
	}
	return projects
}

func ProjectIDsFromReplace(replace map[Dependency]string) []ProjectID {
	projects := make([]ProjectID, 0, len(replace))
	for dep := range replace {
		projects = append(projects, ProjectIDFromDependency(dep))
	}
	return projects
}

// ProjectContains reports whether project owns pkg. The returned subdir is
// the package path inside the project, or "." when pkg names the project root.
func ProjectContains(project ProjectID, pkg RemotePackage) (string, bool) {
	if project.URL != pkg.URL {
		return "", false
	}
	if project.Subdir == "" {
		if pkg.Subdir == "" {
			return ".", true
		}
		return pkg.Subdir, true
	}
	if pkg.Subdir == project.Subdir {
		return ".", true
	}
	prefix := project.Subdir + "/"
	if after, ok := strings.CutPrefix(pkg.Subdir, prefix); ok {
		return after, true
	}
	return "", false
}

// MostSpecificProject chooses the project with the longest matching subdir.
func MostSpecificProject(projects []ProjectID, pkg RemotePackage) (PackageOwner, bool) {
	var best PackageOwner
	bestLen := -1
	for _, project := range projects {
		pkgSubdir, ok := ProjectContains(project, pkg)
		if !ok {
			continue
		}
		if n := len(project.Subdir); n > bestLen {
			best = PackageOwner{Project: project, PackageSubdir: pkgSubdir}
			bestLen = n
		}
	}
	return best, bestLen >= 0
}

// ProjectTag returns the git tag for a project version.
func ProjectTag(project ProjectID, version string) string {
	return project.Dependency().Tag(version)
}
