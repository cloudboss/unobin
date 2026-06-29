// Package defaults lets a Go library type declare schema defaults for
// its inputs in a type-safe, string-free way. A type lists them from a
// Defaults method, referring to its own fields directly:
//
//	func (f File) Defaults() []defaults.Default {
//		return []defaults.Default{
//			defaults.Value(f.Mode, 0o644),
//		}
//	}
//
// Value declares that a field left out of a body takes the given
// value, which the runtime fills in before the type's code runs and
// the plan renders as the real input. A field with no default, and
// not declared as a pointer, is required.
//
// Because the fields are real struct fields, the Go compiler checks
// that they exist, that a rename updates them, and that a Value
// default matches its field's type. unobin reads these declarations
// from source at compile time (goschema); the methods are never
// called, so a returned value identifies the rule but not its field,
// which the compiler supplies from source.
package defaults

// Default is one declared input default, built by Value.
// It is opaque: an author builds it and returns it; unobin reads the
// declaration from source rather than from this value.
type Default struct{}

// Value declares that field takes def when a body leaves it out. The
// shared type parameter ties the default to the field's type.
func Value[T any](field, def T) Default { return Default{} }
