// Package typecheck holds unobin's semantic type model and the
// static type checker that runs after parsing. Type values carry no
// source position so the same value compares cleanly whether it
// came from a written declaration or was inferred from a literal.
//
// The parser's lang.TypeExpr is the syntactic form of a written
// type declaration; convert it to a Type with FromLang before
// reasoning about it here.
package typecheck

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// Kind discriminates the Type variants. The zero value Unknown
// stands in for a type the walker could not determine; the checker
// skips compatibility comparisons involving Unknown so source the
// inferrer cannot reason about fails open rather than producing
// noise.
type Kind int

const (
	Unknown Kind = iota
	Opaque
	String
	Integer
	Number
	Boolean
	Null
	List
	Map
	Object
	Tuple
	Optional
	// Union appears only in builtin function signatures, constructed
	// by their registrations; inference never produces it and no
	// source syntax writes it.
	Union
)

// Type is a structural type description. The Kind field discrim-
// inates which of the value-bearing fields is populated:
//
//   - List/Set/Map/Optional read Elem.
//   - Tuple reads Elems.
//   - Object reads Fields.
//
// Pass Type around by value; the recursive children live on the
// pointer fields so a deeply nested type still copies in constant
// time at the top level.
//
// Open applies to Object only: an open object may hold fields beyond
// the declared ones. Open changes what may be present, never what may
// be read - dotting into an undeclared field stays an error.
type Type struct {
	Kind   Kind
	Open   bool
	Elem   *Type
	Elems  []Type
	Fields []ObjectField
}

// ObjectField is one named field of an Object type. Optional is
// true when the field may be omitted. Defaulted is true when omission
// fills a non-null default, so reads use Type directly instead of
// optional(Type).
type ObjectField struct {
	Name      string
	Type      Type
	Optional  bool
	Defaulted bool
}

func TUnknown() Type { return Type{Kind: Unknown} }
func TOpaque() Type  { return Type{Kind: Opaque} }
func TString() Type  { return Type{Kind: String} }
func TInteger() Type { return Type{Kind: Integer} }
func TNumber() Type  { return Type{Kind: Number} }
func TBoolean() Type { return Type{Kind: Boolean} }
func TNull() Type    { return Type{Kind: Null} }

func TList(elem Type) Type { return Type{Kind: List, Elem: &elem} }
func TMap(elem Type) Type  { return Type{Kind: Map, Elem: &elem} }
func TOptional(elem Type) Type {
	if elem.Kind == Optional {
		return elem
	}
	return Type{Kind: Optional, Elem: &elem}
}

func TTuple(elems []Type) Type {
	return Type{Kind: Tuple, Elems: elems}
}

func TObject(fields []ObjectField) Type {
	return Type{Kind: Object, Fields: fields}
}

func TOpenObject(fields []ObjectField) Type {
	return Type{Kind: Object, Open: true, Fields: fields}
}

func TUnion(members []Type) Type {
	return Type{Kind: Union, Elems: members}
}

// IsKnown returns false when the Type is Unknown or wraps Unknown
// through an Optional. The checker uses this to bail out of
// comparisons it cannot reason about.
func (t Type) IsKnown() bool {
	if t.Kind == Unknown {
		return false
	}
	if t.Kind == Optional && t.Elem != nil {
		return t.Elem.IsKnown()
	}
	return true
}

// ContainsUnknown reports whether t or any type nested inside it is
// Unknown. IsKnown looks only through Optional; this walks every
// element, tuple member, and object field.
func (t Type) ContainsUnknown() bool {
	if t.Kind == Unknown {
		return true
	}
	if t.Elem != nil && t.Elem.ContainsUnknown() {
		return true
	}
	for _, e := range t.Elems {
		if e.ContainsUnknown() {
			return true
		}
	}
	for _, f := range t.Fields {
		if f.Type.ContainsUnknown() {
			return true
		}
	}
	return false
}

// Unwrap returns the inner type when t is Optional, else t itself.
// The checker peels optionality before comparing the underlying
// types.
func (t Type) Unwrap() Type {
	if t.Kind == Optional && t.Elem != nil {
		return *t.Elem
	}
	return t
}

// Field returns the named field of an Object type, ok=false when
// the field is absent or the type is not an Object.
func (t Type) Field(name string) (ObjectField, bool) {
	if t.Kind != Object {
		return ObjectField{}, false
	}
	for _, f := range t.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return ObjectField{}, false
}

// String renders the type in the unobin type vocabulary the
// operator would have written: `list(string)`, `optional(integer)`,
// `object({ a: string  b: integer })`. Object field order follows
// the order the type was constructed in so error messages stay
// stable with respect to the source.
func (t Type) String() string {
	switch t.Kind {
	case Unknown:
		return "unknown"
	case Opaque:
		return "opaque"
	case String:
		return "string"
	case Integer:
		return "integer"
	case Number:
		return "number"
	case Boolean:
		return "boolean"
	case Null:
		return "null"
	case List:
		return "list(" + t.elemString() + ")"
	case Map:
		return "map(" + t.elemString() + ")"
	case Optional:
		return "optional(" + t.elemString() + ")"
	case Tuple:
		parts := make([]string, len(t.Elems))
		for i, e := range t.Elems {
			parts[i] = e.String()
		}
		return "tuple(" + strings.Join(parts, ", ") + ")"
	case Union:
		parts := make([]string, len(t.Elems))
		for i, e := range t.Elems {
			parts[i] = e.String()
		}
		return strings.Join(parts, " | ")
	case Object:
		parts := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			parts[i] = fmt.Sprintf("%s: %s", f.Name, f.Type.String())
		}
		s := "object({ " + strings.Join(parts, "  ") + " })"
		if t.Open {
			return "open(" + s + ")"
		}
		return s
	}
	return fmt.Sprintf("type#%d", t.Kind)
}

func (t Type) elemString() string {
	if t.Elem == nil {
		return "unknown"
	}
	return t.Elem.String()
}

// Equal reports whether two types describe the same thing. It is
// recursive and order-sensitive for tuples; object fields compare
// by name regardless of declaration order so two object types that
// declare the same fields in different orders match.
func (t Type) Equal(other Type) bool {
	if t.Kind != other.Kind {
		return false
	}
	switch t.Kind {
	case List, Map, Optional:
		if t.Elem == nil || other.Elem == nil {
			return t.Elem == other.Elem
		}
		return t.Elem.Equal(*other.Elem)
	case Tuple, Union:
		if len(t.Elems) != len(other.Elems) {
			return false
		}
		for i := range t.Elems {
			if !t.Elems[i].Equal(other.Elems[i]) {
				return false
			}
		}
		return true
	case Object:
		if t.Open != other.Open {
			return false
		}
		if len(t.Fields) != len(other.Fields) {
			return false
		}
		left := sortFields(t.Fields)
		right := sortFields(other.Fields)
		for i := range left {
			if left[i].Name != right[i].Name ||
				left[i].Optional != right[i].Optional ||
				!left[i].Type.Equal(right[i].Type) {
				return false
			}
		}
		return true
	}
	return true
}

func sortFields(fs []ObjectField) []ObjectField {
	out := append([]ObjectField(nil), fs...)
	slices.SortFunc(out, func(a, b ObjectField) int { return cmp.Compare(a.Name, b.Name) })
	return out
}
