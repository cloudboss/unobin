package deps

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"

	"golang.org/x/mod/semver"

	"github.com/cloudboss/unobin/pkg/lang"
	"github.com/cloudboss/unobin/pkg/lang/syntax"
	"github.com/cloudboss/unobin/pkg/toolchain"
)

// ManifestFileName is the standard filename for a factory's dependency
// manifest.
const ManifestFileName = "unobin.manifest"

// SourceManifestFileName is the grammar-first manifest filename.
const SourceManifestFileName = "manifest.ub"

// Manifest is a parsed unobin.manifest. Requires maps each direct
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

// ReadManifest reads and parses manifest.ub or unobin.manifest from fsys.
// A missing file returns an error wrapping fs.ErrNotExist, which callers can
// detect with errors.Is.
func ReadManifest(fsys fs.FS) (*Manifest, error) {
	source, sourceErr := fs.ReadFile(fsys, SourceManifestFileName)
	legacy, legacyErr := fs.ReadFile(fsys, ManifestFileName)
	if sourceErr == nil && legacyErr == nil {
		return nil, fmt.Errorf(
			"manifest: found both %s and %s; keep one manifest file",
			SourceManifestFileName,
			ManifestFileName,
		)
	}
	if sourceErr == nil {
		return parseSourceManifest(source)
	}
	if !errors.Is(sourceErr, fs.ErrNotExist) {
		return nil, sourceErr
	}
	if legacyErr != nil {
		return nil, legacyErr
	}
	return parseLegacyManifest(legacy)
}

func parseLegacyManifest(b []byte) (*Manifest, error) {
	f, err := lang.ParseSource(ManifestFileName, b)
	if err != nil {
		return nil, err
	}
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return parseManifestBody(f)
}

func parseSourceManifest(b []byte) (*Manifest, error) {
	f, err := syntax.ParseSource(SourceManifestFileName, b)
	if err != nil {
		return nil, err
	}
	if errs := syntax.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return parseSourceManifestBody(f)
}

// EncodeManifest renders a manifest as unobin.manifest source. Entries in
// each block are sorted by dependency id for stable diffs. An empty
// requires block is still written; an empty replace block is omitted.
func EncodeManifest(m *Manifest) []byte {
	var b strings.Builder
	if m.UnobinVersion != "" {
		fmt.Fprintf(&b, "unobin-version: '%s'\n", m.UnobinVersion)
	}
	encodeManifestBlock(&b, "requires", m.Requires)
	if len(m.Replace) > 0 {
		encodeManifestBlock(&b, "replace", m.Replace)
	}
	return []byte(b.String())
}

// EncodeSourceManifest renders a parseable grammar-first manifest.ub draft.
func EncodeSourceManifest(m *Manifest) []byte {
	var b strings.Builder
	b.WriteString("manifest: {\n")
	if m.UnobinVersion != "" {
		fmt.Fprintf(&b, "unobin-version: '%s'\n", m.UnobinVersion)
	}
	encodeSourceManifestBlock(&b, "requires", m.Requires)
	if len(m.Replace) > 0 {
		encodeSourceManifestBlock(&b, "replace", m.Replace)
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

func encodeManifestBlock(b *strings.Builder, name string, entries map[Dependency]string) {
	byID := make(map[string]string, len(entries))
	ids := make([]string, 0, len(entries))
	for dep, val := range entries {
		id := dep.String()
		ids = append(ids, id)
		byID[id] = val
	}
	slices.Sort(ids)
	if len(ids) == 0 {
		fmt.Fprintf(b, "%s: {}\n", name)
		return
	}
	fmt.Fprintf(b, "%s: {\n", name)
	for _, id := range ids {
		fmt.Fprintf(b, "  '%s': '%s'\n", id, byID[id])
	}
	b.WriteString("}\n")
}

func encodeSourceManifestBlock(
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

// WriteManifest serializes m and atomically replaces the file at path.
func WriteManifest(path string, m *Manifest) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, EncodeManifest(m), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WriteSourceManifest serializes m as canonical grammar-first manifest.ub
// source and atomically replaces the file at path.
func WriteSourceManifest(path string, m *Manifest) error {
	return lang.WriteCanonical(path, EncodeSourceManifest(m))
}

func parseManifestBody(f *lang.File) (*Manifest, error) {
	m := &Manifest{Requires: map[Dependency]string{}, Replace: map[Dependency]string{}}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != lang.FieldIdent {
			continue
		}
		if fld.Key.Name == "unobin-version" {
			s, ok := fld.Value.(*lang.StringLit)
			if !ok {
				return nil, fmt.Errorf("manifest: unobin-version must be a version string")
			}
			if !semver.IsValid(s.Value) {
				return nil, fmt.Errorf(
					"manifest: unobin-version: %q is not a valid version", s.Value)
			}
			m.UnobinVersion = s.Value
			continue
		}
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			continue
		}
		var err error
		switch fld.Key.Name {
		case "requires":
			m.Requires, err = parseManifestMap(obj, requireSemver)
		case "replace":
			m.Replace, err = parseManifestMap(obj, nil)
		}
		if err != nil {
			return nil, err
		}
	}
	return checkManifestToolchainPin(m)
}

func parseSourceManifestBody(f *syntax.File) (*Manifest, error) {
	m := &Manifest{Requires: map[Dependency]string{}, Replace: map[Dependency]string{}}
	if f == nil || f.Manifest == nil {
		return nil, fmt.Errorf("manifest: %s must declare manifest", SourceManifestFileName)
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
	m.Requires, err = parseSourceManifestRequires(f.Manifest.Requires)
	if err != nil {
		return nil, err
	}
	m.Replace, err = parseSourceManifestReplace(f.Manifest.Replace)
	if err != nil {
		return nil, err
	}
	return checkManifestToolchainPin(m)
}

func parseSourceManifestRequires(
	decls []syntax.ManifestRequire,
) (map[Dependency]string, error) {
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

func parseSourceManifestReplace(
	decls []syntax.ManifestReplace,
) (map[Dependency]string, error) {
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

// parseManifestMap reads a manifest block's entries into a map keyed by
// dependency. checkValue, when non-nil, validates each value string.
func parseManifestMap(
	obj *lang.ObjectLit, checkValue func(id, val string) error,
) (map[Dependency]string, error) {
	out := map[Dependency]string{}
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldString {
			continue
		}
		dep, err := ParseDependency(fld.Key.String)
		if err != nil {
			return nil, fmt.Errorf("manifest: %w", err)
		}
		val, ok := fld.Value.(*lang.StringLit)
		if !ok {
			continue
		}
		if checkValue != nil {
			if err := checkValue(fld.Key.String, val.Value); err != nil {
				return nil, err
			}
		}
		out[dep] = val.Value
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
