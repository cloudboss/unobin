package resolve

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
)

// ExtractImports walks the `imports:` block of f and parses each value
// into an ImportRef. Returns a map of alias to ref plus any per-import
// parse errors. Shape problems with the block itself are reported by
// `lang.ValidateImports` and silently skipped here so the two passes
// don't both report the same errors.
func ExtractImports(f *lang.File) (map[string]ImportRef, []error) {
	obj := topLevelObject(f, "imports")
	if obj == nil {
		return nil, nil
	}
	out := make(map[string]ImportRef, len(obj.Fields))
	var errs []error
	for _, fld := range obj.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		s, ok := fld.Value.(*lang.StringLit)
		if !ok {
			continue
		}
		ref, err := ParseImportRef(s.Value)
		if err != nil {
			errs = append(errs, fmt.Errorf("import %q: %w", fld.Key.Name, err))
			continue
		}
		out[fld.Key.Name] = ref
	}
	return out, errs
}

func topLevelObject(f *lang.File, key string) *lang.ObjectLit {
	if f == nil || f.Body == nil {
		return nil
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind == lang.FieldIdent && fld.Key.Name == key {
			if obj, ok := fld.Value.(*lang.ObjectLit); ok {
				return obj
			}
			return nil
		}
	}
	return nil
}
