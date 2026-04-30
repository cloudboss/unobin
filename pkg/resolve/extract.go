package resolve

import (
	"fmt"

	"github.com/cloudboss/unobin/pkg/lang"
)

// ResolvedImport pairs a parsed ImportRef with the Source the resolver
// returned for it. Source is nil when the resolver errored on this
// alias. The corresponding error is in the returned slice.
type ResolvedImport struct {
	Ref    ImportRef
	Source *Source
}

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

// ResolveImports extracts imports from f and runs each through resolver.
// The returned map preserves every alias even when its Source is nil
// (resolver error). Errors are collected from extraction, same-repo
// version checking, and the resolver itself.
func ResolveImports(f *lang.File, resolver Resolver) (map[string]*ResolvedImport, []error) {
	refs, errs := ExtractImports(f)
	if len(refs) == 0 {
		return nil, errs
	}
	errs = append(errs, CheckSameRepoVersions(refs)...)

	out := make(map[string]*ResolvedImport, len(refs))
	for alias, ref := range refs {
		src, err := resolver.Resolve(ref)
		if err != nil {
			errs = append(errs, fmt.Errorf("import %q: %w", alias, err))
			out[alias] = &ResolvedImport{Ref: ref}
			continue
		}
		out[alias] = &ResolvedImport{Ref: ref, Source: src}
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
