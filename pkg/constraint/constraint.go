// Package constraint lets a Go library type declare cross-field constraints
// on its inputs in a type-safe, string-free way. A type lists them from a
// Constraints method, referring to its own fields directly:
//
//	func (v CertInput) Constraints() []constraint.Constraint {
//		return []constraint.Constraint{
//			constraint.ExactlyOneOf(v.SelfSigned, v.AcmArn, v.PemBundle),
//			constraint.When(constraint.Equals(v.Tier, "prod")).
//				Require(constraint.Present(v.ValidityDays)).
//				Message("production certs need an explicit validity"),
//		}
//	}
//
// Because the fields are real struct fields, the Go compiler checks that
// they exist and a rename updates them, so there are no stale field-name
// strings. unobin reads these declarations from source at compile time
// (goschema) and resolves each field reference to its kebab input name,
// merging them with any constraints written in UB. The methods are not
// evaluated at runtime, so a returned value carries the rule's kind and
// message but not its field names, which the compiler supplies from source.
// The declarative vocabulary matches the one a UB constraints block uses.
package constraint

// Kind classifies a constraint. The set kinds (everything but predicate)
// are about which fields are set; a predicate is a when/require condition.
type Kind int

const (
	KindExactlyOneOf Kind = iota + 1
	KindAtLeastOneOf
	KindAtMostOneOf
	KindRequiredTogether
	KindRequiredWith
	KindForbiddenWith
	KindPredicate
	KindEach
)

// Constraint is one declared rule, built by one of the constructors below.
// It is opaque: an author builds it and returns it; unobin reads the rule
// from source rather than from this value.
type Constraint struct {
	kind    Kind
	message string
}

// Message attaches a custom failure message and returns the updated rule,
// for chaining after a predicate: When(...).Require(...).Message("...").
func (c Constraint) Message(text string) Constraint {
	c.message = text
	return c
}

// ExactlyOneOf requires that exactly one of the fields is set.
func ExactlyOneOf(fields ...any) Constraint { return Constraint{kind: KindExactlyOneOf} }

// AtLeastOneOf requires that at least one of the fields is set.
func AtLeastOneOf(fields ...any) Constraint { return Constraint{kind: KindAtLeastOneOf} }

// AtMostOneOf requires that no more than one of the fields is set.
func AtMostOneOf(fields ...any) Constraint { return Constraint{kind: KindAtMostOneOf} }

// RequiredTogether requires that the fields are all set or all unset.
func RequiredTogether(fields ...any) Constraint { return Constraint{kind: KindRequiredTogether} }

// RequiredWith requires that when field is set, every requires field is set.
func RequiredWith(field any, requires ...any) Constraint {
	return Constraint{kind: KindRequiredWith}
}

// ForbiddenWith requires that when field is set, no forbids field is set.
func ForbiddenWith(field any, forbids ...any) Constraint {
	return Constraint{kind: KindForbiddenWith}
}

// Must is an unconditional predicate: every condition must hold.
func Must(require ...Condition) Constraint { return Constraint{kind: KindPredicate} }

// Each applies per-element rules to a list field. The body receives one
// element and returns the set constraints that must hold for every
// element of the list; a field reference inside the body names the
// element's field (replicas[*].host), and a reference to the receiver
// names a top-level field as usual. A null or empty list checks
// nothing.
//
// As with every constructor in this package, Each is read from source
// and never called: the body declares rules and must be a single
// return of a constraint list.
//
//	constraint.Each(v.Replicas, func(r Replica) []constraint.Constraint {
//		return []constraint.Constraint{
//			constraint.ExactlyOneOf(r.Inline, r.FromFile),
//			constraint.RequiredWith(r.TLS, v.CACert),
//		}
//	})
func Each[T any](list []T, body func(T) []Constraint) Constraint {
	return Constraint{kind: KindEach}
}

// Clause is a predicate whose condition has been stated with When; call
// Require on it to give the requirement that must hold when the condition
// does.
type Clause struct{}

// When begins a conditional predicate. Pair it with Require.
func When(cond Condition) Clause { return Clause{} }

// Require completes a When predicate: when the When condition holds, every
// require condition must hold too.
func (Clause) Require(require ...Condition) Constraint { return Constraint{kind: KindPredicate} }

// Condition is a boolean over a type's inputs, used to build predicates.
// Like Constraint it is opaque and read from source, not evaluated here.
type Condition struct{}

// Equals holds when field equals value.
func Equals(field, value any) Condition { return Condition{} }

// NotEquals holds when field does not equal value.
func NotEquals(field, value any) Condition { return Condition{} }

// IsTrue holds when the boolean field is true.
func IsTrue(field any) Condition { return Condition{} }

// IsFalse holds when the boolean field is false.
func IsFalse(field any) Condition { return Condition{} }

// Present holds when field is set (not null).
func Present(field any) Condition { return Condition{} }

// Absent holds when field is unset (null).
func Absent(field any) Condition { return Condition{} }

// AtLeast holds when field is greater than or equal to value, which may be
// a literal or another field. A null operand makes the condition pass.
func AtLeast(field, value any) Condition { return Condition{} }

// Above holds when field is greater than value (literal or field). A null
// operand makes the condition pass.
func Above(field, value any) Condition { return Condition{} }

// Below holds when field is less than value (literal or field). A null
// operand makes the condition pass.
func Below(field, value any) Condition { return Condition{} }

// AtMost holds when field is less than or equal to value (literal or
// field). A null operand makes the condition pass.
func AtMost(field, value any) Condition { return Condition{} }

// OneOf holds when field's value is one of the given values.
func OneOf(field any, values ...any) Condition { return Condition{} }

// All holds when every condition holds.
func All(conds ...Condition) Condition { return Condition{} }

// Any holds when at least one condition holds.
func Any(conds ...Condition) Condition { return Condition{} }

// Not holds when the condition does not hold.
func Not(cond Condition) Condition { return Condition{} }
