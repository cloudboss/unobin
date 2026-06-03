package lang

import (
	"strconv"
	"strings"
)

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
	if c.Kind != "predicate" && hasSplatField(c.Fields) {
		checkSplatEntry(idx, c, values, evalAgainstInputs, errs)
		return
	}
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

// splatMarker is the reserved segment suffix that stands for every
// element of a list in a constraint field name.
const splatMarker = "[*]"

func hasSplatField(fields []string) bool {
	for _, f := range fields {
		if strings.Contains(f, splatMarker) {
			return true
		}
	}
	return false
}

// splatRuleViolation reports why a fields list that uses [*] is
// malformed: fewer than two fields, or splats of two different lists.
// Empty when the list is fine. Shared by the compile-time validation
// and the checker so both report the same wording.
func splatRuleViolation(fields []string) string {
	if len(fields) < 2 {
		return "a [*] constraint needs at least two fields"
	}
	prefix, found := "", false
	for _, f := range fields {
		before, _, ok := strings.Cut(f, splatMarker)
		if !ok {
			continue
		}
		switch {
		case !found:
			prefix, found = before, true
		case before != prefix:
			return "[*] fields must splat the same list, got " +
				prefix + splatMarker + " and " + before + splatMarker
		}
	}
	return ""
}

// splatPrefix returns the path of the list the fields splat, the part
// before the first [*] of the first splatted field.
func splatPrefix(fields []string) string {
	for _, f := range fields {
		if before, _, ok := strings.Cut(f, splatMarker); ok {
			return before
		}
	}
	return ""
}

// checkSplatEntry runs a set constraint whose fields splat a list once
// per element, with the leftmost [*] replaced by the element's index, so
// a failure names the element that broke the rule (replicas[1].host).
// Every [*] field must splat the same list, and a splatted entry must
// relate at least two fields: a per-element rule over one field only
// restates whether that field is set. An absent or null list checks
// nothing, matching how an unset field reads as null, and each element
// of a nested splat expands recursively.
func checkSplatEntry(
	idx int,
	c ConstraintEntry,
	values map[string]any,
	evalAgainstInputs EvalFunc,
	errs *ErrorList,
) {
	if msg := splatRuleViolation(c.Fields); msg != "" {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (%s [%s]): %s", idx, c.Kind, joinNames(c.Fields), msg)
		return
	}
	prefix := splatPrefix(c.Fields)
	root, ok := lookupPath(values, prefix)
	if !ok || root == nil {
		return
	}
	lst, ok := root.([]any)
	if !ok {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (%s [%s]): cannot splat %s at %s%s",
			idx, c.Kind, joinNames(c.Fields), TypeMessage(root), prefix, splatMarker)
		return
	}
	for i := range lst {
		elem := c
		elem.Fields = make([]string, len(c.Fields))
		for j, f := range c.Fields {
			elem.Fields[j] = strings.Replace(f, splatMarker, "["+strconv.Itoa(i)+"]", 1)
		}
		checkEntry(idx, elem, values, evalAgainstInputs, errs)
	}
}

// ConstraintEntry is one resolved cross-field constraint, independent of
// whether it was parsed from a UB constraints block or derived from a Go
// type at compile time. The set kinds use Fields; a predicate uses When
// and Require (and an optional Message). A Fields name may splat a list
// ([*]) to run the rule once per element. goschema builds these directly
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
					if name, ok := constraintFieldName(el); ok {
						c.Fields = append(c.Fields, name)
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

// constraintFieldName renders a `fields:` element to its input name. A
// bare ident is the name itself; a dotted path names a field inside a
// nested input (code.inline). A segment may index a list element with a
// whole-number literal (listeners[0].cert) or splat every element
// (replicas[*].host); a splat must be followed by a field and may
// appear once per path. ok is false for anything else.
func constraintFieldName(e Expr) (string, bool) {
	switch v := e.(type) {
	case *Ident:
		return v.Name, true
	case *DotPath:
		if v.Root == nil || v.Root.Name == "" {
			return "", false
		}
		var b strings.Builder
		b.WriteString(v.Root.Name)
		splats := 0
		for i, seg := range v.Segments {
			switch {
			case seg.Splat:
				if splats > 0 || i == len(v.Segments)-1 {
					return "", false
				}
				splats++
				b.WriteString(splatMarker)
			case seg.Index != nil:
				n, ok := literalIndex(seg.Index)
				if !ok {
					return "", false
				}
				b.WriteString("[" + strconv.Itoa(n) + "]")
			case seg.Name != "":
				b.WriteString("." + seg.Name)
			default:
				return "", false
			}
		}
		return b.String(), true
	}
	return "", false
}

// literalIndex reads an index expression as a whole-number literal.
func literalIndex(e Expr) (int, bool) {
	n, ok := e.(*NumberLit)
	if !ok || n.IsFloat || n.ParsedInt < 0 {
		return 0, false
	}
	return int(n.ParsedInt), true
}

// lookupPath reads a field value by its dotted name, stepping into
// nested maps for a name like "code.inline" and into list elements for
// an indexed segment like "listeners[0].cert". A name with no dot is a
// single lookup, identical to the flat form. found is false when any
// segment is absent, an index is out of range, or a parent value is not
// the right container, so an unset nested field reads the same as an
// unset top-level one.
func lookupPath(values map[string]any, name string) (any, bool) {
	cur := any(values)
	for seg := range strings.SplitSeq(name, ".") {
		base, indexes, ok := splitIndexes(seg)
		if !ok {
			return nil, false
		}
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		v, ok := m[base]
		if !ok {
			return nil, false
		}
		cur = v
		for _, idx := range indexes {
			lst, ok := cur.([]any)
			if !ok || idx >= len(lst) {
				return nil, false
			}
			cur = lst[idx]
		}
	}
	return cur, true
}

// splitIndexes splits one path segment into its field name and any
// trailing [N] indexes, so "matrix[0][1]" yields ("matrix", [0, 1]).
// ok is false for a malformed segment: an empty name, an unclosed
// bracket, or a non-numeric index, [*] included, since expansion
// replaces every splat with a concrete index before lookup.
func splitIndexes(seg string) (string, []int, bool) {
	base, rest, found := strings.Cut(seg, "[")
	if !found {
		return seg, nil, true
	}
	if base == "" {
		return "", nil, false
	}
	var indexes []int
	for {
		num, after, found := strings.Cut(rest, "]")
		if !found {
			return "", nil, false
		}
		n, err := strconv.Atoi(num)
		if err != nil || n < 0 {
			return "", nil, false
		}
		indexes = append(indexes, n)
		if after == "" {
			return base, indexes, true
		}
		rest, found = strings.CutPrefix(after, "[")
		if !found {
			return "", nil, false
		}
	}
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
