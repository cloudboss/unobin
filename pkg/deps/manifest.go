package deps

import (
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
	"golang.org/x/mod/semver"
)

// ManifestFileName is the standard filename for a factory's dependency
// manifest.
const ManifestFileName = "unobin.manifest"

// Manifest is a parsed unobin.manifest. Requires maps each direct
// dependency to the lowest version (a git tag) the factory accepts for
// it; resolution may select a higher one to satisfy the whole set. The
// factory's own version is its git tag, not recorded here.
type Manifest struct {
	Requires map[Dependency]string
}

// ReadManifest reads and parses unobin.manifest from fsys. A missing
// file returns an error wrapping fs.ErrNotExist, which callers can
// detect with errors.Is.
func ReadManifest(fsys fs.FS) (*Manifest, error) {
	b, err := fs.ReadFile(fsys, ManifestFileName)
	if err != nil {
		return nil, err
	}
	f, err := lang.ParseSource(ManifestFileName, b)
	if err != nil {
		return nil, err
	}
	if errs := lang.ValidateFile(f); errs.Len() > 0 {
		return nil, errs.Err()
	}
	return parseManifestBody(f)
}

// EncodeManifest renders a manifest as unobin.manifest source. The
// requires entries are sorted by dependency id for stable diffs.
func EncodeManifest(m *Manifest) []byte {
	byID := make(map[string]string, len(m.Requires))
	ids := make([]string, 0, len(m.Requires))
	for dep, version := range m.Requires {
		id := dep.String()
		ids = append(ids, id)
		byID[id] = version
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return []byte("requires: {}\n")
	}
	var b strings.Builder
	b.WriteString("requires: {\n")
	for _, id := range ids {
		fmt.Fprintf(&b, "  '%s': '%s'\n", id, byID[id])
	}
	b.WriteString("}\n")
	return []byte(b.String())
}

// WriteManifest serializes m and atomically replaces the file at path.
func WriteManifest(path string, m *Manifest) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, EncodeManifest(m), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func parseManifestBody(f *lang.File) (*Manifest, error) {
	m := &Manifest{Requires: map[Dependency]string{}}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "requires" {
			continue
		}
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			continue
		}
		for _, req := range obj.Fields {
			if req.Key.Kind != lang.FieldString {
				continue
			}
			dep, err := ParseDependency(req.Key.String)
			if err != nil {
				return nil, fmt.Errorf("manifest: %w", err)
			}
			val, ok := req.Value.(*lang.StringLit)
			if !ok {
				continue
			}
			if !semver.IsValid(val.Value) {
				return nil, fmt.Errorf("manifest: dependency %q: %q is not a valid version",
					req.Key.String, val.Value)
			}
			m.Requires[dep] = val.Value
		}
	}
	return m, nil
}
