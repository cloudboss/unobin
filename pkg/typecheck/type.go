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
	"fmt"
	"sort"
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
	Any
	String
	Integer
	Number
	Boolean
	Null
	List
	Set
	Map
	Object
	Tuple
	Optional
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
type Type struct {
	Kind   Kind
	Elem   *Type
	Elems  []Type
	Fields []ObjectField
}

// ObjectField is one named field of an Object type. Optional is
// true when the field may be absent (e.g. it came from a *T Go
// field or an optional() declaration). Defaulted is true when an
// input declaration provides a default, so a missing or null value
// is replaced before anything reads it.
type ObjectField struct {
	Name      string
	Type      Type
	Optional  bool
	Defaulted bool
}

func TUnknown() Type { return Type{Kind: Unknown} }
func TAny() Type     { return Type{Kind: Any} }
func TString() Type  { return Type{Kind: String} }
func TInteger() Type { return Type{Kind: Integer} }
func TNumber() Type  { return Type{Kind: Number} }
func TBoolean() Type { return Type{Kind: Boolean} }
func TNull() Type    { return Type{Kind: Null} }

func TList(elem Type) Type { return Type{Kind: List, Elem: &elem} }
func TSet(elem Type) Type  { return Type{Kind: Set, Elem: &elem} }
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
	case Any:
		return "any"
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
	case Set:
		return "set(" + t.elemString() + ")"
	case Map:
		return "map(" + t.elemString() + ")"
	case Optional:
		return "optional(" + t.elemString() + ")"
	case Tuple:
		parts := make([]string, len(t.Elems))
		for i, e := range t.Elems {
			parts[i] = e.String()
		}
		return "tuple([" + strings.Join(parts, " ") + "])"
	case Object:
		parts := make([]string, len(t.Fields))
		for i, f := range t.Fields {
			parts[i] = fmt.Sprintf("%s: %s", f.Name, f.Type.String())
		}
		return "object({ " + strings.Join(parts, "  ") + " })"
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
	case List, Set, Map, Optional:
		if t.Elem == nil || other.Elem == nil {
			return t.Elem == other.Elem
		}
		return t.Elem.Equal(*other.Elem)
	case Tuple:
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
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
