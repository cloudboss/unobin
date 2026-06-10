package lang

// TopLevelBlock returns the object value of a file's top-level field
// with the given name. A nil file, an absent field, or a value of
// another form yields nil; ValidateFile reports the wrong-form case,
// so callers may treat nil as absence.
func TopLevelBlock(f *File, name string) *ObjectLit {
	if f == nil || f.Body == nil {
		return nil
	}
	for _, fld := range f.Body.Fields {
		if fld.Key.Kind != FieldIdent || fld.Key.Name != name {
			continue
		}
		obj, _ := fld.Value.(*ObjectLit)
		return obj
	}
	return nil
}

// FieldMap returns an object's plain fields by name: every field with
// an identifier key that is not a meta key. A nil object yields an
// empty map.
func FieldMap(obj *ObjectLit) map[string]Expr {
	out := map[string]Expr{}
	if obj == nil {
		return out
	}
	for _, fld := range obj.Fields {
		if fld.Key.Kind != FieldIdent || fld.Key.IsMeta() {
			continue
		}
		out[fld.Key.Name] = fld.Value
	}
	return out
}
