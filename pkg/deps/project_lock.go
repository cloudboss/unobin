package deps

import (
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
)

// ProjectLockFileName is the dependency project-lock filename.
const ProjectLockFileName = "project-lock.ub"

// CurrentProjectLockVersion is the schema version this package reads and writes.
// A different version errors on read, since this build cannot guarantee a
// correct interpretation.
const CurrentProjectLockVersion = 1

// ProjectLockKind records how a selected dependency's integrity is guaranteed:
// a `ub` dependency holds a content hash of its source tree, while a `go`
// dependency rides the generated module's go.sum and records only a commit.
type ProjectLockKind string

const (
	ProjectLockKindGo ProjectLockKind = "go"
	ProjectLockKindUB ProjectLockKind = "ub"
)

// ProjectLock is the on-disk schema for the dependency project-lock file. It
// pins the full resolved set so compiles are reproducible without re-running
// selection. Deps is keyed by dependency id, a repository URL with an optional
// `//subdir`, the same form a project `requires:` key uses.
type ProjectLock struct {
	Version          int
	ToolchainVersion string
	Deps             map[string]*ProjectLockDep
}

// ProjectLockDep is one resolved dependency. Version is the selected git tag;
// the floor lives in project.ub and is never copied here. Hash is the
// source-tree content hash for `ub` dependencies and is omitted for `go`.
type ProjectLockDep struct {
	Kind    ProjectLockKind
	Version string
	Commit  string
	Hash    string
}

// NewProjectLock returns an empty project-lock at the current schema version.
func NewProjectLock() *ProjectLock {
	return &ProjectLock{
		Version: CurrentProjectLockVersion,
		Deps:    map[string]*ProjectLockDep{},
	}
}

// SortedIDs returns the dependency ids in sorted order.
func (l *ProjectLock) SortedIDs() []string {
	ids := make([]string, 0, len(l.Deps))
	for id := range l.Deps {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

// RepoVersions maps each selected dependency id to its selected version.
// Compile feeds this to the import walk so versionless imports resolve at the
// selected version.
func (l *ProjectLock) RepoVersions() (map[string]string, error) {
	out := make(map[string]string, len(l.Deps))
	for id, entry := range l.Deps {
		if _, err := ParseDependency(id); err != nil {
			return nil, fmt.Errorf("project-lock id %q: %w", id, err)
		}
		out[id] = entry.Version
	}
	return out, nil
}

// ReadProjectLock reads and parses project-lock.ub from fsys. A missing file
// returns an error wrapping fs.ErrNotExist, which callers can detect with
// errors.Is.
func ReadProjectLock(fsys fs.FS) (*ProjectLock, error) {
	source, err := fs.ReadFile(fsys, ProjectLockFileName)
	if err != nil {
		return nil, err
	}
	return DecodeProjectLock(source)
}

// DecodeProjectLock parses a grammar-first project-lock.ub from bytes.
func DecodeProjectLock(b []byte) (*ProjectLock, error) {
	f, err := syntax.ParseSource(ProjectLockFileName, b)
	if err != nil {
		return nil, err
	}
	if errs := syntax.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return parseProjectLockBody(f)
}

func parseProjectLockBody(f *syntax.File) (*ProjectLock, error) {
	if f == nil || f.ProjectLock == nil {
		return nil, fmt.Errorf("project-lock: %s must declare project-lock", ProjectLockFileName)
	}
	projectLock := &ProjectLock{
		Version: int(f.ProjectLock.Version.ParsedInt),
		Deps:    make(map[string]*ProjectLockDep, len(f.ProjectLock.Deps)),
	}
	if f.ProjectLock.Toolchain != nil && f.ProjectLock.Toolchain.UnobinVersion != nil {
		projectLock.ToolchainVersion = f.ProjectLock.Toolchain.UnobinVersion.Value
	}
	for _, dep := range f.ProjectLock.Deps {
		selected := &ProjectLockDep{
			Kind:    ProjectLockKind(dep.Kind.Name),
			Version: dep.Version.Value,
			Commit:  dep.Commit.Value,
		}
		if dep.Hash != nil {
			selected.Hash = dep.Hash.Value
		}
		projectLock.Deps[dep.ID.Value] = selected
	}
	if err := validateProjectLock(projectLock); err != nil {
		return nil, err
	}
	return projectLock, nil
}

// WriteProjectLock serializes projectLock as canonical project-lock.ub source
// and atomically replaces the file at path.
func WriteProjectLock(path string, projectLock *ProjectLock) error {
	b, err := EncodeProjectLock(projectLock)
	if err != nil {
		return err
	}
	return writeProjectLockBytes(path, b)
}

func writeProjectLockBytes(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// EncodeProjectLock serializes projectLock as canonical project-lock.ub source.
func EncodeProjectLock(projectLock *ProjectLock) ([]byte, error) {
	if err := validateProjectLock(projectLock); err != nil {
		return nil, err
	}
	var b strings.Builder
	fmt.Fprintf(&b,
		"project-lock: { version: %d toolchain: { unobin-version: %s } deps: { ",
		projectLock.Version,
		sourceString(projectLock.ToolchainVersion),
	)
	for _, id := range projectLock.SortedIDs() {
		dep := projectLock.Deps[id]
		fmt.Fprintf(&b, "%s: { kind: %s version: %s commit: %s",
			sourceString(id), dep.Kind, sourceString(dep.Version), sourceString(dep.Commit))
		if dep.Hash != "" {
			fmt.Fprintf(&b, " hash: %s", sourceString(dep.Hash))
		}
		b.WriteString(" } ")
	}
	b.WriteString("} }")
	out, err := lang.Canonicalize(ProjectLockFileName, []byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("project-lock: %w", err)
	}
	return out, nil
}

func sourceString(s string) string {
	repl := strings.NewReplacer(
		"\\", "\\\\",
		"'", "\\'",
		"\n", `\n`,
		"\t", `\t`,
		"\r", `\r`,
		"\x00", `\0`,
	)
	return "'" + repl.Replace(s) + "'"
}

func validateProjectLock(l *ProjectLock) error {
	if l == nil {
		return fmt.Errorf("project-lock: nil project-lock")
	}
	if l.Version != CurrentProjectLockVersion {
		return fmt.Errorf("project-lock: unsupported version %d (this build expects %d)",
			l.Version, CurrentProjectLockVersion)
	}
	if l.ToolchainVersion == "" {
		return fmt.Errorf("project-lock: missing toolchain unobin-version")
	}
	if err := validateProjectLockDeps(l); err != nil {
		return err
	}
	if err := CheckNoReplacementSentinelInProjectLock(l); err != nil {
		return err
	}
	for id, dep := range l.Deps {
		if dep.Kind == ProjectLockKindUB && !hasHashAlgorithm(dep.Hash) {
			return fmt.Errorf("project-lock: dependency %q: hash must include an algorithm prefix", id)
		}
	}
	return nil
}

func hasHashAlgorithm(hash string) bool {
	for i, r := range hash {
		if r == ':' {
			return i > 0 && i < len(hash)-1
		}
	}
	return false
}

func validateProjectLockDeps(l *ProjectLock) error {
	if l == nil {
		return fmt.Errorf("project-lock: nil project-lock")
	}
	if l.Deps == nil {
		return fmt.Errorf("project-lock: missing `deps`")
	}
	for id, dep := range l.Deps {
		if _, err := ParseDependency(id); err != nil {
			return fmt.Errorf("project-lock: dependency %q: %w", id, err)
		}
		if dep == nil {
			return fmt.Errorf("project-lock: dependency %q: nil entry", id)
		}
		switch dep.Kind {
		case ProjectLockKindGo, ProjectLockKindUB:
		case "":
			return fmt.Errorf("project-lock: dependency %q: missing `kind`", id)
		default:
			return fmt.Errorf("project-lock: dependency %q: unknown kind %q", id, dep.Kind)
		}
		if dep.Version == "" {
			return fmt.Errorf("project-lock: dependency %q: missing `version`", id)
		}
		if dep.Commit == "" {
			return fmt.Errorf("project-lock: dependency %q: missing `commit`", id)
		}
		switch dep.Kind {
		case ProjectLockKindUB:
			if dep.Hash == "" {
				return fmt.Errorf("project-lock: dependency %q: ub dependency missing `hash`", id)
			}
		case ProjectLockKindGo:
			if dep.Hash != "" {
				return fmt.Errorf("project-lock: dependency %q: go dependency must not set `hash`", id)
			}
		}
	}
	return nil
}
