package cfg

import (
	"reflect"

	"github.com/cloudboss/unobin/pkg/lang"
)

// Field describes one field of a configuration struct, for the
// factory's own help output.
type Field struct {
	Name        string
	Type        string
	Optional    bool
	Description string

	// Fields lists an object-typed field's own fields in declaration
	// order, so help output can show nested structure. Empty for any
	// other type.
	Fields []Field
}

// Describe lists the fields of the configuration struct behind ct in
// declaration order, walking the value New returns the same way the
// decoder does: kebab-case names, a pointer field is optional, and a
// nested struct reads as an object whose own fields fill Fields.
// Anonymous fields are skipped here; Decode rejects them.
// Descriptions come from whatever the zero value sets. Returns nil
// when there is no configuration to describe.
func Describe(ct Registration) []Field {
	if ct == nil {
		return nil
	}
	v := reflect.ValueOf(ct.NewAny())
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	return describeFields(v, map[reflect.Type]bool{})
}

// describeFields walks one struct value's fields. visiting holds the
// struct types on the current path, so a type that nests itself
// through a pointer stops expanding instead of recursing without end.
func describeFields(v reflect.Value, visiting map[reflect.Type]bool) []Field {
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	if visiting[t] {
		return nil
	}
	visiting[t] = true
	defer delete(visiting, t)
	var out []Field
	for f := range t.Fields() {
		if !f.IsExported() || f.Anonymous {
			continue
		}
		ft, optional := f.Type, false
		fv := v.FieldByIndex(f.Index)
		if ft.Kind() == reflect.Pointer {
			optional = true
			ft = ft.Elem()
			if fv.IsNil() {
				fv = reflect.Value{}
			} else {
				fv = fv.Elem()
			}
		}
		out = append(out, Field{
			Name:        lang.PascalToKebab(f.Name),
			Type:        typeLabel(ft),
			Optional:    optional,
			Description: descriptionOf(fv),
			Fields:      objectFields(ft, fv, visiting),
		})
	}
	return out
}

// objectFields expands an object-typed field into its own fields: a
// plain nested struct directly, or the Value an Object wrapper holds.
// An unallocated optional struct expands from a fresh zero value, so
// the structure shows even when New leaves the pointer nil.
func objectFields(t reflect.Type, v reflect.Value, visiting map[reflect.Type]bool) []Field {
	if implementsValue(t) {
		if !t.Implements(objectKindType) {
			return nil
		}
		inner, ok := t.FieldByName("Value")
		if !ok || inner.Type.Kind() != reflect.Struct {
			return nil
		}
		iv := reflect.Value{}
		if v.IsValid() {
			iv = v.FieldByName("Value")
		}
		if !iv.IsValid() {
			iv = reflect.New(inner.Type).Elem()
		}
		return describeFields(iv, visiting)
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	if !v.IsValid() {
		v = reflect.New(t).Elem()
	}
	return describeFields(v, visiting)
}

// typeLabel renders a configuration field's type the way the language
// writes it. Containers name their element; a nested struct is an
// object whose own fields stay unflattened.
func typeLabel(t reflect.Type) string {
	if implementsValue(t) {
		switch {
		case t.Implements(objectKindType):
			return "object"
		case t.Implements(listKindType):
			return "list(" + elementLabel(t) + ")"
		case t.Implements(mapKindType):
			return "map(" + elementLabel(t) + ")"
		}
		switch t {
		case stringType:
			return "string"
		case integerType:
			return "integer"
		case numberType:
			return "number"
		case booleanType:
			return "boolean"
		}
		return "?"
	}
	if t.Kind() == reflect.Struct {
		return "object"
	}
	return "?"
}

func elementLabel(t reflect.Type) string {
	el, ok := t.FieldByName("Element")
	if !ok {
		return "?"
	}
	return typeLabel(el.Type)
}

// descriptionOf reads the Description string a wrapper's zero value
// set, when the field value is reachable.
func descriptionOf(v reflect.Value) string {
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return ""
	}
	d := v.FieldByName("Description")
	if !d.IsValid() || d.Kind() != reflect.String {
		return ""
	}
	return d.String()
}
