package deps

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/toolchain"
)

// ProjectFileName is the dependency project filename.
const ProjectFileName = "project.ub"

// Requirement is a root dependency floor declared by project.ub.
type Requirement struct {
	Version  string
	Indirect bool
}

// Project is a parsed dependency project. Requires maps each dependency
// to the lowest version (a git tag) the factory accepts for it; resolution
// may select a higher one to satisfy the whole set. The factory's own
// version is its git tag, not recorded here. Replace maps a dependency's
// URL to a local path: the resolver reads that dependency from the path
// instead of fetching it, and the dependency needs no floor or project-lock entry.
// UnobinVersion, when set, pins the toolchain: only a CLI of exactly that
// version compiles the project, since the unobin runtime is not a
// dependency whose version resolution selects.
type Project struct {
	UnobinVersion string
	Requires      map[Dependency]Requirement
	Replace       map[Dependency]string
}

func (m *Project) RequireVersions() map[Dependency]string {
	out := make(map[Dependency]string, len(m.Requires))
	for dep, req := range m.Requires {
		out[dep] = req.Version
	}
	return out
}

func (m *Project) DirectRequireVersions() map[Dependency]string {
	out := map[Dependency]string{}
	for dep, req := range m.Requires {
		if req.Indirect {
			continue
		}
		out[dep] = req.Version
	}
	return out
}

func (m *Project) DirectCount() int {
	count := 0
	for _, req := range m.Requires {
		if !req.Indirect {
			count++
		}
	}
	return count
}

func (m *Project) IndirectCount() int {
	count := 0
	for _, req := range m.Requires {
		if req.Indirect {
			count++
		}
	}
	return count
}

func (m *Project) SetRequire(dep Dependency, version string, indirect bool) {
	if m.Requires == nil {
		m.Requires = map[Dependency]Requirement{}
	}
	m.Requires[dep] = Requirement{Version: version, Indirect: indirect}
}

// ReadProject reads and parses project.ub from fsys. A missing file
// returns an error wrapping fs.ErrNotExist, which callers can detect with
// errors.Is.
func ReadProject(fsys fs.FS) (*Project, error) {
	b, err := fs.ReadFile(fsys, ProjectFileName)
	if err != nil {
		return nil, err
	}
	return parseProject(b)
}

func parseProject(b []byte) (*Project, error) {
	f, err := syntax.ParseSource(ProjectFileName, b)
	if err != nil {
		return nil, err
	}
	if errs := syntax.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return parseProjectBody(f)
}

// EncodeProject renders a parseable project.ub draft.
func EncodeProject(m *Project) []byte {
	var b strings.Builder
	b.WriteString("project: {\n")
	if m.UnobinVersion != "" {
		fmt.Fprintf(&b, "unobin-version: '%s'\n", m.UnobinVersion)
	}
	encodeRequireBlock(&b, m.Requires)
	if len(m.Replace) > 0 {
		encodeStringBlock(&b, "replace", m.Replace)
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

func encodeRequireBlock(b *strings.Builder, entries map[Dependency]Requirement) {
	byID := make(map[string]Requirement, len(entries))
	ids := make([]string, 0, len(entries))
	for dep, req := range entries {
		id := dep.String()
		ids = append(ids, id)
		byID[id] = req
	}
	slices.Sort(ids)
	b.WriteString("requires: {\n")
	for _, id := range ids {
		req := byID[id]
		fmt.Fprintf(b, "'%s': {\n", id)
		fmt.Fprintf(b, "version: '%s'\n", req.Version)
		if req.Indirect {
			b.WriteString("indirect: true\n")
		}
		b.WriteString("}\n")
	}
	b.WriteString("}\n")
}

func encodeStringBlock(
	b *strings.Builder,
	name string,
	entries map[Dependency]string,
) {
	byID := make(map[string]string, len(entries))
	ids := make([]string, 0, len(entries))
	for dep, val := range entries {
		id := dep.String()
		ids = append(ids, id)
		byID[id] = val
	}
	slices.Sort(ids)
	fmt.Fprintf(b, "%s: {\n", name)
	for _, id := range ids {
		fmt.Fprintf(b, "'%s': '%s'\n", id, byID[id])
	}
	b.WriteString("}\n")
}

// WriteProject serializes m as canonical project.ub source and atomically
// replaces the file at path.
func WriteProject(path string, m *Project) error {
	return lang.WriteCanonical(path, EncodeProject(m))
}

func parseProjectBody(f *syntax.File) (*Project, error) {
	m := &Project{Requires: map[Dependency]Requirement{}, Replace: map[Dependency]string{}}
	if f == nil || f.Project == nil {
		return nil, fmt.Errorf("project: %s must declare project", ProjectFileName)
	}
	if f.Project.UnobinVersion != nil {
		version := f.Project.UnobinVersion.Value
		if !semver.IsValid(version) {
			return nil, fmt.Errorf(
				"project: unobin-version: %q is not a valid version", version)
		}
		m.UnobinVersion = version
	}
	var err error
	m.Requires, err = parseProjectRequires(f.Project.Requires)
	if err != nil {
		return nil, err
	}
	m.Replace, err = parseProjectReplace(f.Project.Replace)
	if err != nil {
		return nil, err
	}
	return checkProjectToolchainPin(m)
}

func parseProjectRequires(decls []syntax.ProjectRequire) (map[Dependency]Requirement, error) {
	out := map[Dependency]Requirement{}
	for _, decl := range decls {
		dep, err := ParseDependency(decl.ID.Value)
		if err != nil {
			return nil, fmt.Errorf("project: %w", err)
		}
		if decl.Version == nil {
			return nil, fmt.Errorf("project: dependency %q: missing version", decl.ID.Value)
		}
		version := decl.Version.Value
		if err := requireSemver(decl.ID.Value, version); err != nil {
			return nil, err
		}
		out[dep] = Requirement{
			Version:  version,
			Indirect: decl.Indirect != nil && decl.Indirect.Value,
		}
	}
	return out, nil
}

func parseProjectReplace(decls []syntax.ProjectReplace) (map[Dependency]string, error) {
	out := map[Dependency]string{}
	for _, decl := range decls {
		dep, err := ParseDependency(decl.ID.Value)
		if err != nil {
			return nil, fmt.Errorf("project: %w", err)
		}
		out[dep] = decl.Path.Value
	}
	return out, nil
}

func checkProjectToolchainPin(m *Project) (*Project, error) {
	for dep := range m.Requires {
		if dep.URL == toolchain.UnobinModulePath {
			return nil, fmt.Errorf(
				"project: %s is toolchain-versioned; pin it with the project file's"+
					" unobin-version line, not requires", dep.URL)
		}
	}
	return m, nil
}

func requireSemver(id, val string) error {
	if !semver.IsValid(val) {
		return fmt.Errorf("project: dependency %q: %q is not a valid version", id, val)
	}
	return nil
}
