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
}

// Describe lists the fields of the configuration struct behind ct in
// declaration order, walking the value New returns the same way the
// decoder does: kebab-case names, a pointer field is optional, and a
// nested struct reads as an object without flattening its fields.
// Descriptions come from whatever the zero value sets. Returns nil
// when there is no configuration to describe.
func Describe(ct *ConfigurationType) []Field {
	if ct == nil || ct.New == nil {
		return nil
	}
	v := reflect.ValueOf(ct.New())
	if !v.IsValid() {
		return nil
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	var out []Field
	for f := range t.Fields() {
		if !f.IsExported() {
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
		})
	}
	return out
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
