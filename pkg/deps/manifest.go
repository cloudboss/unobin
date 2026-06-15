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

// ManifestFileName is the dependency manifest filename.
const ManifestFileName = "manifest.ub"

// Manifest is a parsed dependency manifest. Requires maps each direct
// dependency to the lowest version (a git tag) the factory accepts for
// it; resolution may select a higher one to satisfy the whole set. The
// factory's own version is its git tag, not recorded here. Replace maps a
// dependency's URL to a local path: the resolver reads that dependency
// from the path instead of fetching it, and the dependency needs no
// floor or lock entry. UnobinVersion, when set, pins the toolchain:
// only a CLI of exactly that version compiles the project, since the
// unobin runtime is not a dependency whose version resolution selects.
type Manifest struct {
	UnobinVersion string
	Requires      map[Dependency]string
	Replace       map[Dependency]string
}

// ReadManifest reads and parses manifest.ub from fsys. A missing file
// returns an error wrapping fs.ErrNotExist, which callers can detect with
// errors.Is.
func ReadManifest(fsys fs.FS) (*Manifest, error) {
	b, err := fs.ReadFile(fsys, ManifestFileName)
	if err != nil {
		return nil, err
	}
	return parseManifest(b)
}

func parseManifest(b []byte) (*Manifest, error) {
	f, err := syntax.ParseSource(ManifestFileName, b)
	if err != nil {
		return nil, err
	}
	if errs := syntax.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return parseManifestBody(f)
}

// EncodeManifest renders a parseable manifest.ub draft.
func EncodeManifest(m *Manifest) []byte {
	var b strings.Builder
	b.WriteString("manifest: {\n")
	if m.UnobinVersion != "" {
		fmt.Fprintf(&b, "unobin-version: '%s'\n", m.UnobinVersion)
	}
	encodeManifestBlock(&b, "requires", m.Requires)
	if len(m.Replace) > 0 {
		encodeManifestBlock(&b, "replace", m.Replace)
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

func encodeManifestBlock(
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

// WriteManifest serializes m as canonical manifest.ub source and atomically
// replaces the file at path.
func WriteManifest(path string, m *Manifest) error {
	return lang.WriteCanonical(path, EncodeManifest(m))
}

func parseManifestBody(f *syntax.File) (*Manifest, error) {
	m := &Manifest{Requires: map[Dependency]string{}, Replace: map[Dependency]string{}}
	if f == nil || f.Manifest == nil {
		return nil, fmt.Errorf("manifest: %s must declare manifest", ManifestFileName)
	}
	if f.Manifest.UnobinVersion != nil {
		version := f.Manifest.UnobinVersion.Value
		if !semver.IsValid(version) {
			return nil, fmt.Errorf(
				"manifest: unobin-version: %q is not a valid version", version)
		}
		m.UnobinVersion = version
	}
	var err error
	m.Requires, err = parseManifestRequires(f.Manifest.Requires)
	if err != nil {
		return nil, err
	}
	m.Replace, err = parseManifestReplace(f.Manifest.Replace)
	if err != nil {
		return nil, err
	}
	return checkManifestToolchainPin(m)
}

func parseManifestRequires(decls []syntax.ManifestRequire) (map[Dependency]string, error) {
	out := map[Dependency]string{}
	for _, decl := range decls {
		dep, err := ParseDependency(decl.ID.Value)
		if err != nil {
			return nil, fmt.Errorf("manifest: %w", err)
		}
		if err := requireSemver(decl.ID.Value, decl.Version.Value); err != nil {
			return nil, err
		}
		out[dep] = decl.Version.Value
	}
	return out, nil
}

func parseManifestReplace(decls []syntax.ManifestReplace) (map[Dependency]string, error) {
	out := map[Dependency]string{}
	for _, decl := range decls {
		dep, err := ParseDependency(decl.ID.Value)
		if err != nil {
			return nil, fmt.Errorf("manifest: %w", err)
		}
		out[dep] = decl.Path.Value
	}
	return out, nil
}

func checkManifestToolchainPin(m *Manifest) (*Manifest, error) {
	for dep := range m.Requires {
		if dep.URL == toolchain.UnobinModulePath {
			return nil, fmt.Errorf(
				"manifest: %s is toolchain-versioned; pin it with the manifest's"+
					" unobin-version line, not requires", dep.URL)
		}
	}
	return m, nil
}

func requireSemver(id, val string) error {
	if !semver.IsValid(val) {
		return fmt.Errorf("manifest: dependency %q: %q is not a valid version", id, val)
	}
	return nil
}
