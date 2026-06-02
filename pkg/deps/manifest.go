package deps

import (
	"fmt"
	"io/fs"

	"github.com/cloudboss/unobin/pkg/lang"
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
			m.Requires[dep] = val.Value
		}
	}
	return m, nil
}
