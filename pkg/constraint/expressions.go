package constraint

// Length counts Unicode code points, list elements, or map entries.
func Length(value any) any { return nil }

// ByteLength counts bytes in a string or byte value.
func ByteLength(value any) any { return nil }

// Remainder gives the integer remainder. A null operand yields null.
func Remainder(value, divisor any) any { return nil }

// Lookup reads a list index or map key. A missing element yields null.
func Lookup(collection, key any) any { return nil }

// HasKey tests whether a map contains a key, including keys with null values.
func HasKey(collection, key any) Condition { return Condition{} }

// HasItem tests whether a list contains a scalar value.
func HasItem(collection, value any) Condition { return Condition{} }

// IsUnique tests whether the scalar values in a list are distinct.
func IsUnique(collection any) Condition { return Condition{} }

// Sum adds the numbers in a list.
func Sum(collection any) any { return nil }

// Project evaluates a projection for each list element or map entry.
func Project[T any](collection any, projection func(T) any) any { return nil }

// CountWhere counts the list elements or map entries that satisfy a condition.
func CountWhere[T any](collection any, predicate func(T) Condition) any { return nil }

// Binding names a field available to an Expr predicate.
type Binding struct{}

// Bind makes a Go selector available as input.name within an Expr predicate.
func Bind(name string, field any) Binding { return Binding{} }

// Expr declares an Unobin Boolean expression over explicitly bound input fields.
func Expr(source string, bindings ...Binding) Condition { return Condition{} }
