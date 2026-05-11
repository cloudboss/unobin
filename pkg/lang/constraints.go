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
		switch c.kind {
		case "exactly-one-of":
			checkExactlyOneOf(i, c.fields, values, errs)
		case "at-least-one-of":
			checkAtLeastOneOf(i, c.fields, values, errs)
		case "at-most-one-of", "mutually-exclusive":
			checkAtMostOneOf(i, c.kind, c.fields, values, errs)
		case "required-together":
			checkRequiredTogether(i, c.fields, values, errs)
		case "required-with":
			checkRequiredWith(i, c.fields, values, errs)
		case "forbidden-with":
			checkForbiddenWith(i, c.fields, values, errs)
		case "predicate":
			checkPredicate(i, c, values, evalAgainstInputs, errs)
		}
	}
	return errs
}

type constraintEntry struct {
	kind    string
	fields  []string
	when    Expr
	require Expr
	message string
}

func readConstraint(obj *ObjectLit) (constraintEntry, bool) {
	var c constraintEntry
	for _, f := range obj.Fields {
		if f.Key.Kind != FieldIdent {
			continue
		}
		switch f.Key.Name {
		case "kind":
			if id, ok := f.Value.(*Ident); ok {
				c.kind = id.Name
			}
		case "fields":
			if arr, ok := f.Value.(*ArrayLit); ok {
				for _, el := range arr.Elements {
					if id, ok := el.(*Ident); ok {
						c.fields = append(c.fields, id.Name)
					}
				}
			}
		case "when":
			c.when = f.Value
		case "require":
			c.require = f.Value
		case "message":
			if s, ok := f.Value.(*StringLit); ok {
				c.message = s.Value
			}
		}
	}
	if c.kind == "" {
		return c, false
	}
	return c, true
}

// nonNullFields returns the names of the listed fields whose values
// are present and not null. A field absent from the map counts as
// null; ValidateInputs always populates every declared input, so
// missing keys here mean the constraint references an input the
// stack does not declare (caught by ValidateConstraintReferences).
func nonNullFields(fields []string, values map[string]any) []string {
	var nn []string
	for _, f := range fields {
		if v, ok := values[f]; ok && v != nil {
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
	tv, ok := values[trigger]
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
	tv, ok := values[trigger]
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
		v, ok := values[f]
		if !ok || v == nil {
			miss = append(miss, f)
		}
	}
	return miss
}

func checkPredicate(
	idx int,
	c constraintEntry,
	values map[string]any,
	evalAgainstInputs EvalFunc,
	errs *ErrorList,
) {
	if c.when == nil || c.require == nil {
		return
	}
	if evalAgainstInputs == nil {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (predicate): cannot evaluate (no evaluator provided)", idx)
		return
	}
	whenVal, err := evalAgainstInputs(c.when)
	if err != nil {
		errs.Addf(ErrSchema, c.when.Span().Start,
			"constraints[%d] (predicate): when: %v", idx, err)
		return
	}
	whenBool, ok := whenVal.(bool)
	if !ok {
		errs.Addf(ErrSchema, c.when.Span().Start,
			"constraints[%d] (predicate): when must evaluate to a boolean, got %T", idx, whenVal)
		return
	}
	if !whenBool {
		return
	}
	requireVal, err := evalAgainstInputs(c.require)
	if err != nil {
		errs.Addf(ErrSchema, c.require.Span().Start,
			"constraints[%d] (predicate): require: %v", idx, err)
		return
	}
	requireBool, ok := requireVal.(bool)
	if !ok {
		errs.Addf(ErrSchema, c.require.Span().Start,
			"constraints[%d] (predicate): require must evaluate to a boolean, got %T", idx, requireVal)
		return
	}
	if !requireBool {
		msg := c.message
		if msg == "" {
			msg = "predicate requirement not satisfied"
		}
		errs.Addf(ErrSchema, Position{}, "constraints[%d] (predicate): %s", idx, msg)
	}
}

func joinNames(names []string) string {
	return strings.Join(names, ", ")
}
