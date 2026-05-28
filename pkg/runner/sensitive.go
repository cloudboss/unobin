package runner

import "slices"

import "github.com/cloudboss/unobin/pkg/lang"

// sensitivePlaceholder is the literal renderers print in place of a
// masked value.
const sensitivePlaceholder = "***"

// stringSetContains reports whether s appears in slice. The slice is
// small (per-step field counts) so a linear scan is cheaper than
// allocating a map for one lookup.
func stringSetContains(slice []string, s string) bool {
	return slices.Contains(slice, s)
}

// rootSensitiveOutputs returns the set of root output names declared
// with `@sensitive: true` in the factory source's `outputs:` block.
// Returns an empty set for a nil file or a file with no outputs
// block.
func rootSensitiveOutputs(f *lang.File) map[string]bool {
	if f == nil || f.Body == nil {
		return map[string]bool{}
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "outputs" {
			continue
		}
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			return map[string]bool{}
		}
		return lang.SensitiveOutputs(obj)
	}
	return map[string]bool{}
}
