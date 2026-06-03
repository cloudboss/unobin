package lang

import "strings"

// CheckConstraints evaluates the value-level cross-field constraints
// in a stack's `constraints:` block against the validated input
// values. Predicate constraints use evalAgainstInputs to reduce their
// `when:` and `require:` expressions; pass nil to skip predicate
// evaluation. Callers should run ValidateConstraints first; malformed
// entries that slip through are skipped silently.
func CheckConstraints(
	block *ArrayLit,
	values map[string]any,
	evalAgainstInputs EvalFunc,
) *ErrorList {
	errs := NewErrorList(0)
	if block == nil {
		return errs
	}
	for i, e := range block.Elements {
		obj, ok := e.(*ObjectLit)
		if !ok {
			continue
		}
		c, ok := readConstraint(obj)
		if !ok {
			continue
		}
		checkEntry(i, c, values, evalAgainstInputs, errs)
	}
	return errs
}

// CheckConstraintEntries checks already-resolved constraint entries, such
// as those goschema derives from a Go type at compile time, against the
// values. It is the same check CheckConstraints runs after parsing a UB
// block, so Go-derived and UB constraints behave identically.
func CheckConstraintEntries(
	entries []ConstraintEntry,
	values map[string]any,
	evalAgainstInputs EvalFunc,
) *ErrorList {
	errs := NewErrorList(0)
	for i, c := range entries {
		checkEntry(i, c, values, evalAgainstInputs, errs)
	}
	return errs
}

func checkEntry(
	idx int,
	c ConstraintEntry,
	values map[string]any,
	evalAgainstInputs EvalFunc,
	errs *ErrorList,
) {
	switch c.Kind {
	case "exactly-one-of":
		checkExactlyOneOf(idx, c.Fields, values, errs)
	case "at-least-one-of":
		checkAtLeastOneOf(idx, c.Fields, values, errs)
	case "at-most-one-of", "mutually-exclusive":
		checkAtMostOneOf(idx, c.Kind, c.Fields, values, errs)
	case "required-together":
		checkRequiredTogether(idx, c.Fields, values, errs)
	case "required-with":
		checkRequiredWith(idx, c.Fields, values, errs)
	case "forbidden-with":
		checkForbiddenWith(idx, c.Fields, values, errs)
	case "predicate":
		checkPredicate(idx, c, values, evalAgainstInputs, errs)
	}
}

// ConstraintEntry is one resolved cross-field constraint, independent of
// whether it was parsed from a UB constraints block or derived from a Go
// type at compile time. The set kinds use Fields; a predicate uses When
// and Require (and an optional Message). goschema builds these directly
// for Go library types so they run through the same check as UB ones.
type ConstraintEntry struct {
	Kind    string
	Fields  []string
	When    Expr
	Require Expr
	Message string
}

func readConstraint(obj *ObjectLit) (ConstraintEntry, bool) {
	var c ConstraintEntry
	for _, f := range obj.Fields {
		if f.Key.Kind != FieldIdent {
			continue
		}
		switch f.Key.Name {
		case "kind":
			if id, ok := f.Value.(*Ident); ok {
				c.Kind = id.Name
			}
		case "fields":
			if arr, ok := f.Value.(*ArrayLit); ok {
				for _, el := range arr.Elements {
					if id, ok := el.(*Ident); ok {
						c.Fields = append(c.Fields, id.Name)
					}
				}
			}
		case "when":
			c.When = f.Value
		case "require":
			c.Require = f.Value
		case "message":
			if s, ok := f.Value.(*StringLit); ok {
				c.Message = s.Value
			}
		}
	}
	if c.Kind == "" {
		return c, false
	}
	return c, true
}

// lookupPath reads a field value by its dotted name, stepping into
// nested maps for a name like "code.inline". A name with no dot is a
// single lookup, identical to the flat form. found is false when any
// segment is absent or a parent value is not a map, so an unset nested
// field reads the same as an unset top-level one.
func lookupPath(values map[string]any, name string) (any, bool) {
	cur := any(values)
	for seg := range strings.SplitSeq(name, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := m[seg]
		if !ok {
			return nil, false
		}
		cur = v
	}
	return cur, true
}

// nonNullFields returns the names of the listed fields whose values
// are present and not null. A field that resolves to no value, whether
// a missing top-level key or an unset segment of a nested path, counts
// as null.
func nonNullFields(fields []string, values map[string]any) []string {
	var nn []string
	for _, f := range fields {
		if v, ok := lookupPath(values, f); ok && v != nil {
			nn = append(nn, f)
		}
	}
	return nn
}

func checkExactlyOneOf(idx int, fields []string, values map[string]any, errs *ErrorList) {
	nn := nonNullFields(fields, values)
	if len(nn) != 1 {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (exactly-one-of [%s]): expected exactly one to be set, got %d (%s)",
			idx, joinNames(fields), len(nn), joinNames(nn))
	}
}

func checkAtLeastOneOf(idx int, fields []string, values map[string]any, errs *ErrorList) {
	nn := nonNullFields(fields, values)
	if len(nn) == 0 {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (at-least-one-of [%s]): expected at least one to be set, got none",
			idx, joinNames(fields))
	}
}

func checkAtMostOneOf(
	idx int, kind string, fields []string, values map[string]any, errs *ErrorList,
) {
	nn := nonNullFields(fields, values)
	if len(nn) > 1 {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (%s [%s]): expected at most one to be set, got %d (%s)",
			idx, kind, joinNames(fields), len(nn), joinNames(nn))
	}
}

func checkRequiredTogether(idx int, fields []string, values map[string]any, errs *ErrorList) {
	nn := nonNullFields(fields, values)
	if len(nn) != 0 && len(nn) != len(fields) {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (required-together [%s]): expected all set or all null, got %d set (%s)",
			idx, joinNames(fields), len(nn), joinNames(nn))
	}
}

// checkRequiredWith reads the first field as the trigger; if it is
// set, every other field must also be set. The semantics match TF's
// RequiredWith and let an author say "if A is provided, B and C must
// be too" without coupling B and C to each other.
func checkRequiredWith(idx int, fields []string, values map[string]any, errs *ErrorList) {
	if len(fields) < 2 {
		return
	}
	trigger := fields[0]
	rest := fields[1:]
	tv, ok := lookupPath(values, trigger)
	if !ok || tv == nil {
		return
	}
	missing := nonNullMissing(rest, values)
	if len(missing) > 0 {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (required-with): %q is set, so [%s] must also be set; missing %s",
			idx, trigger, joinNames(rest), joinNames(missing))
	}
}

// checkForbiddenWith reads the first field as the trigger; if it is
// set, every other field must be null. The mirror of required-with.
func checkForbiddenWith(idx int, fields []string, values map[string]any, errs *ErrorList) {
	if len(fields) < 2 {
		return
	}
	trigger := fields[0]
	rest := fields[1:]
	tv, ok := lookupPath(values, trigger)
	if !ok || tv == nil {
		return
	}
	present := nonNullFields(rest, values)
	if len(present) > 0 {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (forbidden-with): %q is set, so [%s] must be null; got %s",
			idx, trigger, joinNames(rest), joinNames(present))
	}
}

func nonNullMissing(fields []string, values map[string]any) []string {
	var miss []string
	for _, f := range fields {
		v, ok := lookupPath(values, f)
		if !ok || v == nil {
			miss = append(miss, f)
		}
	}
	return miss
}

func checkPredicate(
	idx int,
	c ConstraintEntry,
	values map[string]any,
	evalAgainstInputs EvalFunc,
	errs *ErrorList,
) {
	if c.When == nil || c.Require == nil {
		return
	}
	if evalAgainstInputs == nil {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (predicate): cannot evaluate (no evaluator provided)", idx)
		return
	}
	whenVal, err := evalAgainstInputs(c.When)
	if err != nil {
		errs.Addf(ErrSchema, c.When.Span().Start,
			"constraints[%d] (predicate): when: %v", idx, err)
		return
	}
	whenBool, ok := whenVal.(bool)
	if !ok {
		errs.Addf(ErrSchema, c.When.Span().Start,
			"constraints[%d] (predicate): when must evaluate to a boolean, got %s",
			idx, TypeMessage(whenVal))
		return
	}
	if !whenBool {
		return
	}
	requireVal, err := evalAgainstInputs(c.Require)
	if err != nil {
		errs.Addf(ErrSchema, c.Require.Span().Start,
			"constraints[%d] (predicate): require: %v", idx, err)
		return
	}
	requireBool, ok := requireVal.(bool)
	if !ok {
		errs.Addf(ErrSchema, c.Require.Span().Start,
			"constraints[%d] (predicate): require must evaluate to a boolean, got %s",
			idx, TypeMessage(requireVal))
		return
	}
	if !requireBool {
		msg := c.Message
		if msg == "" {
			msg = "predicate requirement not satisfied"
		}
		errs.Addf(ErrSchema, Position{}, "constraints[%d] (predicate): %s", idx, msg)
	}
}

func joinNames(names []string) string {
	return strings.Join(names, ", ")
}

// ConstraintSpec is the embeddable, string-only form of a constraint. The
// predicate When and Require are kept as unobin source so the whole set
// can be generated into a factory and parsed back at plan time; a set
// constraint leaves both empty and uses Fields. goschema produces these
// from a Go type, and codegen bakes them into the factory.
type ConstraintSpec struct {
	Kind    string
	Fields  []string
	When    string
	Require string
	Message string
}

// ParseSpecs parses each spec's When and Require source into expressions
// and returns entries ready for CheckConstraintEntries. A set constraint
// (empty When and Require) yields an entry with nil expressions. Parse
// errors are collected; a spec that fails to parse is skipped.
func ParseSpecs(specs []ConstraintSpec) ([]ConstraintEntry, *ErrorList) {
	errs := NewErrorList(0)
	entries := make([]ConstraintEntry, 0, len(specs))
	for _, s := range specs {
		e := ConstraintEntry{Kind: s.Kind, Fields: s.Fields, Message: s.Message}
		when, ok := parseSpecExpr(s.When, "when", errs)
		if !ok {
			continue
		}
		require, ok := parseSpecExpr(s.Require, "require", errs)
		if !ok {
			continue
		}
		e.When = when
		e.Require = require
		entries = append(entries, e)
	}
	return entries, errs
}

func parseSpecExpr(src, label string, errs *ErrorList) (Expr, bool) {
	if src == "" {
		return nil, true
	}
	expr, err := ParseExpr("constraint", []byte(src))
	if err != nil {
		errs.Addf(ErrSchema, Position{}, "constraint %s %q: %v", label, src, err)
		return nil, false
	}
	return expr, true
}
