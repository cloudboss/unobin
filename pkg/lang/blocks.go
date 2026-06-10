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
