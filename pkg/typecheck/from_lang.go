package typecheck

import (
	"github.com/cloudboss/unobin/pkg/lang"
)

// FromLang turns a parsed lang.TypeExpr into a semantic Type.
// Returns TUnknown when the input is nil or names a constructor the
// converter does not understand; the checker treats Unknown as a
// silent skip.
func FromLang(t lang.TypeExpr) Type {
	switch v := t.(type) {
	case nil:
		return TUnknown()
	case *lang.TypeAtomic:
		return atomicFromLang(v.Name)
	case *lang.TypeList:
		return TList(FromLang(v.Elem))
	case *lang.TypeMap:
		return TMap(FromLang(v.Elem))
	case *lang.TypeTuple:
		elems := make([]Type, len(v.Elements))
		for i, e := range v.Elements {
			elems[i] = FromLang(e)
		}
		return TTuple(elems)
	case *lang.TypeObject:
		fields := make([]ObjectField, 0, len(v.Fields))
		for _, f := range v.Fields {
			fields = append(fields, objectFieldFromLang(f))
		}
		return TObject(fields)
	case *lang.TypeOptional:
		return TOptional(FromLang(v.Elem))
	}
	return TUnknown()
}

func atomicFromLang(name string) Type {
	switch name {
	case "string":
		return TString()
	case "integer":
		return TInteger()
	case "number":
		return TNumber()
	case "boolean":
		return TBoolean()
	case "null":
		return TNull()
	case "any":
		return TAny()
	}
	return TUnknown()
}

// objectFieldFromLang converts a TypeObjectField, which may use
// either a bare type or a full input declaration. An input
// declaration is unwrapped through typeFromInputDecl below; the
// resulting Optional wrapper becomes the field's `optional()`
// marker on ObjectField.Optional, and a declared default sets
// Defaulted so readers see the inner type, the way a defaulted
// top-level input reads.
func objectFieldFromLang(f *lang.TypeObjectField) ObjectField {
	var inner lang.TypeExpr
	var optional, defaulted bool
	switch {
	case f.Type != nil:
		inner, optional, defaulted = peelOptional(f.Type)
	case f.Decl != nil:
		inner, optional, defaulted = typeFromInputDecl(f.Decl)
	}
	if inner == nil {
		return ObjectField{Name: f.Name, Type: TUnknown(), Optional: optional}
	}
	return ObjectField{
		Name:      f.Name,
		Type:      FromLang(inner),
		Optional:  optional,
		Defaulted: defaulted,
	}
}

func peelOptional(t lang.TypeExpr) (lang.TypeExpr, bool, bool) {
	if opt, ok := t.(*lang.TypeOptional); ok {
		return opt.Elem, true, opt.Default != nil
	}
	return t, false, false
}

// typeFromInputDecl walks an input declaration object literal (the
// `{ type: ...  description: ...  ... }` form) and pulls out the
// `type:` field, promoting it to a TypeExpr. The booleans report
// whether the declaration is wrapped in `optional()` and whether the
// wrapper provides a default.
func typeFromInputDecl(decl *lang.ObjectLit) (lang.TypeExpr, bool, bool) {
	if decl == nil {
		return nil, false, false
	}
	for _, fld := range decl.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.Name != "type" {
			continue
		}
		t, err := lang.PromoteType(fld.Value)
		if err != nil {
			return nil, false, false
		}
		if opt, ok := t.(*lang.TypeOptional); ok {
			return opt.Elem, true, opt.Default != nil
		}
		return t, false, false
	}
	return nil, false, false
}

// InputsFromBlock walks an `inputs:` block object literal and
// returns each input's name to its semantic type. An input declared
// with `optional()` holds the inner type with Optional set; Defaulted
// is set too when the wrapper provides a default. A reader treats a
// defaulted input as its inner type (the default replaces a missing
// or null value), while a writer may still omit it or pass null.
func InputsFromBlock(decl *lang.ObjectLit) []ObjectField {
	if decl == nil {
		return nil
	}
	var fields []ObjectField
	for _, fld := range decl.Fields {
		if fld.Key.Kind != lang.FieldIdent || fld.Key.IsMeta() {
			continue
		}
		obj, ok := fld.Value.(*lang.ObjectLit)
		if !ok {
			continue
		}
		inner, optional, defaulted := typeFromInputDecl(obj)
		var t Type
		if inner == nil {
			t = TUnknown()
		} else {
			t = FromLang(inner)
		}
		fields = append(fields, ObjectField{
			Name:      fld.Key.Name,
			Type:      t,
			Optional:  optional,
			Defaulted: defaulted,
		})
	}
	return fields
}
