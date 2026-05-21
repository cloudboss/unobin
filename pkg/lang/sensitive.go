package lang

// SensitiveInputs returns the set of input names declared with
// `@sensitive: true` in an `inputs:` block. Nil block yields an
// empty map.
func SensitiveInputs(block *ObjectLit) map[string]bool {
	out := map[string]bool{}
	if block == nil {
		return out
	}
	for _, fld := range block.Fields {
		if fld.Key.Kind != FieldIdent || fld.Key.IsMeta() {
			continue
		}
		decl, ok := fld.Value.(*ObjectLit)
		if !ok {
			continue
		}
		if hasSensitiveMarker(decl) {
			out[fld.Key.Name] = true
		}
	}
	return out
}

// SensitiveOutputs returns the set of output names declared with
// `@sensitive: true` inside their wrapper in an `outputs:` block.
// Nil block yields an empty map. The block must already have been
// passed through ValidateOutputs; entries that are not wrapper
// objects are skipped silently.
func SensitiveOutputs(block *ObjectLit) map[string]bool {
	out := map[string]bool{}
	if block == nil {
		return out
	}
	for _, fld := range block.Fields {
		if fld.Key.Kind != FieldIdent || fld.Key.IsMeta() {
			continue
		}
		obj, ok := fld.Value.(*ObjectLit)
		if !ok {
			continue
		}
		if hasSensitiveMarker(obj) {
			out[fld.Key.Name] = true
		}
	}
	return out
}

// hasSensitiveMarker reports whether an object literal carries
// `@sensitive: true`. Used by both input declarations and output
// wrappers, which share the same meta-key form.
func hasSensitiveMarker(obj *ObjectLit) bool {
	for _, df := range obj.Fields {
		if df.Key.Kind != FieldIdent || !df.Key.IsMeta() {
			continue
		}
		if df.Key.Name != "@sensitive" {
			continue
		}
		if b, ok := df.Value.(*BoolLit); ok {
			return b.Value
		}
	}
	return false
}
