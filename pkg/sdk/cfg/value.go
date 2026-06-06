// Package cfg is the typed vocabulary a Go library uses to declare
// its configuration. Each wrapper carries the decoded value alongside
// its schema info (description, default, validation) for one field.
// Only wrappers in this package satisfy the Value constraint, so a
// library author cannot place a bare Go type as the element of a
// List, Map, or Set.
package cfg

// Value is the constraint for any type that may appear as the
// element of a List, Map, or Set, or as a standalone field on a
// Configuration. The unexported marker keeps the constraint closed
// to wrappers defined here.
type Value interface {
	isUbValue()
}

type String struct {
	Value       string
	Description string
	Default     string
	Validate    Validator
}

func (String) isUbValue() {}

type Integer struct {
	Value       int64
	Description string
	Default     int64
	Validate    Validator
}

func (Integer) isUbValue() {}

type Number struct {
	Value       float64
	Description string
	Default     float64
	Validate    Validator
}

func (Number) isUbValue() {}

type Boolean struct {
	Value       bool
	Description string
	Default     bool
}

func (Boolean) isUbValue() {}

type Null struct {
	Description string
}

func (Null) isUbValue() {}

type Any struct {
	Value       any
	Description string
	Validate    Validator
}

func (Any) isUbValue() {}

// Element supplies the schema info (description, default, validator)
// applied to each item. Its own Value is ignored.
type List[T Value] struct {
	Value       []T
	Description string
	Default     []T
	Element     T
	Validate    Validator
}

func (List[T]) isUbValue() {}
func (List[T]) isUbList()  {}

type Map[T Value] struct {
	Value       map[string]T
	Description string
	Default     map[string]T
	Element     T
	Validate    Validator
}

func (Map[T]) isUbValue() {}
func (Map[T]) isUbMap()   {}

// Object wraps a user struct in a position that requires a Value,
// such as the element type of a List or Map. T must be a struct
// whose fields are wrapper types or nested structs; the schema
// walker rejects any other field type at library load. An optional
// nested struct at the top level of a Configuration uses a plain
// *Struct and does not need this wrapper.
type Object[T any] struct {
	Value       T
	Description string
	Validate    Validator
}

func (Object[T]) isUbValue()  {}
func (Object[T]) isUbObject() {}
