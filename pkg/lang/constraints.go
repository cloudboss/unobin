package lang

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"
)

// FieldDisplay selects how a constraint diagnostic spells field names.
// A factory-level check keeps the var root, the name an operator sets;
// a node-scoped check (a Go type's spec, a composite's own block)
// prints names relative to the node, matching the keys its body is
// written with. Lookup always uses the rooted name; only the message
// changes.
type FieldDisplay int

const (
	DisplayRooted FieldDisplay = iota
	DisplayNodeRelative
)

// name renders one field reference for a diagnostic.
func (d FieldDisplay) name(f string) string {
	if d == DisplayNodeRelative {
		return strings.TrimPrefix(f, "var.")
	}
	return f
}

// names renders a field list for a diagnostic.
func (d FieldDisplay) names(fields []string) []string {
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = d.name(f)
	}
	return out
}

// EachValue is one iteration's @each binding, handed to the evaluator
// alongside a constraint expression when the entry iterates with
// @for-each: the element index or map key, and the element itself.
type EachValue struct {
	Key   any
	Value any
}

// EachBinding is one named iteration binding in scope for a constraint
// expression: @each for the bare @for-each form, or a declared name
// like @rule for a level of a chained one. Either way the binding is a
// key/value record, read as @name.key and @name.value.
type EachBinding struct {
	Name  string
	Key   any
	Value any
}

// ConstraintEvalFunc reduces a constraint's when, require, or
// @for-each expression against the input values. binds holds the
// iteration bindings in scope, outermost first; it is empty outside
// iteration.
type ConstraintEvalFunc func(e Expr, binds []EachBinding) (any, error)

// ForEachLevel is one link of a chained @for-each: the binding name,
// @-spelled, and the iterable its elements come from. InText is the
// iterable's source form, used to name the element a failure is about.
// The bare form is a single level binding @each.
type ForEachLevel struct {
	Name   string
	In     Expr
	InText string
}

// ForEachSpecLevel is the embeddable form of one chain level, with the
// iterable kept as unobin source the way a spec's when and require are.
type ForEachSpecLevel struct {
	Name string
	In   string
}

// CheckConstraints evaluates the value-level cross-field constraints
// in a stack's `constraints:` block against the validated input
// values. Predicate constraints use evalAgainstInputs to reduce their
// `when:` and `require:` expressions; pass nil to skip predicate
// evaluation. Callers should run ValidateConstraints first; malformed
// entries that slip through are skipped silently.
func CheckConstraints(
	block *ArrayLit,
	values map[string]any,
	evalAgainstInputs ConstraintEvalFunc,
	display FieldDisplay,
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
		checkEntry(i, c, values, evalAgainstInputs, display, errs)
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
	evalAgainstInputs ConstraintEvalFunc,
	display FieldDisplay,
) *ErrorList {
	errs := NewErrorList(0)
	for i, c := range entries {
		checkEntry(i, c, values, evalAgainstInputs, display, errs)
	}
	return errs
}

// CheckConstraintEntry checks one resolved constraint entry against the
// values, reporting any failure under index idx, the entry's position
// in its type's constraint list, so a diagnostic names the same entry
// no matter which entries the caller checks.
func CheckConstraintEntry(
	idx int,
	c ConstraintEntry,
	values map[string]any,
	evalAgainstInputs ConstraintEvalFunc,
	display FieldDisplay,
) *ErrorList {
	errs := NewErrorList(0)
	checkEntry(idx, c, values, evalAgainstInputs, display, errs)
	return errs
}

// ConstraintFieldRoots returns the names of the inputs a constraint
// reads: the first path segment of each fields: entry for a set
// constraint, and of every var reference in the when, require, and
// @for-each expressions for a predicate. The names are sorted and
// unique.
func ConstraintFieldRoots(c ConstraintEntry) []string {
	roots := map[string]struct{}{}
	for _, f := range c.Fields {
		rest, ok := strings.CutPrefix(f, "var.")
		if !ok {
			continue
		}
		root, _, _ := strings.Cut(rest, ".")
		root, _, _ = strings.Cut(root, "[")
		roots[root] = struct{}{}
	}
	exprs := []Expr{c.When, c.Require}
	for _, lv := range c.Levels {
		exprs = append(exprs, lv.In)
	}
	for _, e := range exprs {
		Walk(e, func(x Expr) {
			dp, ok := x.(*DotPath)
			if !ok || dp.Root == nil || dp.Root.Name != "var" || len(dp.Segments) == 0 {
				return
			}
			if name := dp.Segments[0].Name; name != "" {
				roots[name] = struct{}{}
			}
		})
	}
	out := make([]string, 0, len(roots))
	for r := range roots {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

func checkEntry(
	idx int,
	c ConstraintEntry,
	values map[string]any,
	evalAgainstInputs ConstraintEvalFunc,
	display FieldDisplay,
	errs *ErrorList,
) {
	if c.Kind != "predicate" && hasSplatField(c.Fields) {
		checkSplatEntry(idx, c, values, evalAgainstInputs, display, errs)
		return
	}
	if check, ok := fieldsConstraintCheckers[c.Kind]; ok {
		check(idx, c.Fields, values, display, errs)
		return
	}
	if c.Kind == "predicate" {
		checkPredicate(idx, c, values, evalAgainstInputs, display, errs)
	}
}

// fieldsConstraintCheckers binds each `fields:`-based constraint kind
// to its checker. Validation accepts exactly these kinds plus
// predicate, so the accepted vocabulary and the dispatch cannot
// drift apart.
var fieldsConstraintCheckers = map[string]func(
	int, []string, map[string]any, FieldDisplay, *ErrorList,
){
	"exactly-one-of":    checkExactlyOneOf,
	"at-least-one-of":   checkAtLeastOneOf,
	"at-most-one-of":    checkAtMostOneOf,
	"required-together": checkRequiredTogether,
	"required-with":     checkRequiredWith,
	"forbidden-with":    checkForbiddenWith,
}

// FieldsConstraintKinds returns the kinds a `fields:`-based constraint
// entry may declare, sorted. goschema's constructor table is held to
// this set by test.
func FieldsConstraintKinds() []string {
	return slices.Sorted(maps.Keys(fieldsConstraintCheckers))
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
	evalAgainstInputs ConstraintEvalFunc,
	display FieldDisplay,
	errs *ErrorList,
) {
	if msg := splatRuleViolation(display.names(c.Fields)); msg != "" {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (%s [%s]): %s",
			idx, c.Kind, joinNames(display.names(c.Fields)), msg)
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
			idx, c.Kind, joinNames(display.names(c.Fields)), TypeMessage(root),
			display.name(prefix), splatMarker)
		return
	}
	for i := range lst {
		elem := c
		elem.Fields = make([]string, len(c.Fields))
		for j, f := range c.Fields {
			elem.Fields[j] = strings.Replace(f, splatMarker, "["+strconv.Itoa(i)+"]", 1)
		}
		checkEntry(idx, elem, values, evalAgainstInputs, display, errs)
	}
}

// ConstraintEntry is one resolved cross-field constraint, independent of
// whether it was parsed from a UB constraints block or derived from a Go
// type at compile time. The set kinds use Fields; a predicate uses When
// and Require (and an optional Message). A Fields name may splat a list
// ([*]) to run the rule once per element. A predicate may iterate with
// Levels, checking when and require once per element of each level's
// iterable with the level's binding set: a single @each level for the
// bare @for-each form, named levels for a chained one. goschema builds
// these directly for Go library types so they run through the same
// check as UB ones.
type ConstraintEntry struct {
	Kind    string
	Fields  []string
	When    Expr
	Require Expr
	Message string
	Levels  []ForEachLevel
}

func readConstraint(obj *ObjectLit) (ConstraintEntry, bool) {
	var c ConstraintEntry
	for _, f := range obj.Fields {
		if f.Key.Kind != FieldIdent {
			continue
		}
		switch f.Key.Name {
		case "@for-each":
			levels, ok := readForEachLevels(f.Value)
			if !ok {
				return c, false
			}
			c.Levels = levels
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

// constraintFieldName renders a `fields:` element to the var reference
// it must be: var.vpc-id names an input and var.code.inline a field
// inside a nested one, the same spelling every other reference position
// uses. A segment may index a list element with a whole-number literal
// (var.listeners[0].cert) or splat every element (var.replicas[*].host);
// a splat must be followed by a field and may appear once per path. ok
// is false for anything else, a bare name included.
func constraintFieldName(e Expr) (string, bool) {
	v, ok := e.(*DotPath)
	if !ok || v.Root == nil || v.Root.Name != "var" || len(v.Segments) == 0 {
		return "", false
	}
	// The first segment must name an input; an index or splat applies
	// to a field, never to var itself.
	if v.Segments[0].Name == "" {
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

// literalIndex reads an index expression as a whole-number literal.
func literalIndex(e Expr) (int, bool) {
	n, ok := e.(*NumberLit)
	if !ok || n.IsFloat || n.ParsedInt < 0 {
		return 0, false
	}
	return int(n.ParsedInt), true
}

// lookupPath reads a field value by its var reference, stepping into
// nested maps for a name like "var.code.inline" and into list elements
// for an indexed segment like "var.listeners[0].cert". The leading var
// segment resolves to the values themselves; it is part of the name so
// a field reads the same everywhere, never var-stripped in one place
// and rooted in another. found is false for a name not rooted at var,
// or when any segment is absent, an index is out of range, or a parent
// value is not the right container, so an unset nested field reads the
// same as an unset top-level one.
func lookupPath(values map[string]any, name string) (any, bool) {
	rest, ok := strings.CutPrefix(name, "var.")
	if !ok {
		return nil, false
	}
	cur := any(values)
	for seg := range strings.SplitSeq(rest, ".") {
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

func checkExactlyOneOf(
	idx int, fields []string, values map[string]any, display FieldDisplay, errs *ErrorList,
) {
	nn := nonNullFields(fields, values)
	if len(nn) != 1 {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (exactly-one-of [%s]): expected exactly one to be set, got %d (%s)",
			idx, joinNames(display.names(fields)), len(nn), joinNames(display.names(nn)))
	}
}

func checkAtLeastOneOf(
	idx int, fields []string, values map[string]any, display FieldDisplay, errs *ErrorList,
) {
	nn := nonNullFields(fields, values)
	if len(nn) == 0 {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (at-least-one-of [%s]): expected at least one to be set, got none",
			idx, joinNames(display.names(fields)))
	}
}

func checkAtMostOneOf(
	idx int, fields []string, values map[string]any,
	display FieldDisplay, errs *ErrorList,
) {
	nn := nonNullFields(fields, values)
	if len(nn) > 1 {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (at-most-one-of [%s]): expected at most one to be set, got %d (%s)",
			idx, joinNames(display.names(fields)), len(nn), joinNames(display.names(nn)))
	}
}

func checkRequiredTogether(
	idx int, fields []string, values map[string]any, display FieldDisplay, errs *ErrorList,
) {
	nn := nonNullFields(fields, values)
	if len(nn) != 0 && len(nn) != len(fields) {
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (required-together [%s]): expected all set or all null, got %d set (%s)",
			idx, joinNames(display.names(fields)), len(nn), joinNames(display.names(nn)))
	}
}

// checkRequiredWith reads the first field as the trigger; if it is
// set, every other field must also be set. The semantics match TF's
// RequiredWith and let an author say "if A is provided, B and C must
// be too" without coupling B and C to each other.
func checkRequiredWith(
	idx int, fields []string, values map[string]any, display FieldDisplay, errs *ErrorList,
) {
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
			idx, display.name(trigger), joinNames(display.names(rest)),
			joinNames(display.names(missing)))
	}
}

// checkForbiddenWith reads the first field as the trigger; if it is
// set, every other field must be null. The mirror of required-with.
func checkForbiddenWith(
	idx int, fields []string, values map[string]any, display FieldDisplay, errs *ErrorList,
) {
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
			idx, display.name(trigger), joinNames(display.names(rest)),
			joinNames(display.names(present)))
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
	evalAgainstInputs ConstraintEvalFunc,
	display FieldDisplay,
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
	checkPredicateLevels(idx, c, c.Levels, nil, "", evalAgainstInputs, display, errs)
}

// checkPredicateLevels iterates the remaining levels depth-first,
// accumulating bindings and the element text a failure names. With no
// levels left, the when/require pair runs once with every enclosing
// binding in scope.
func checkPredicateLevels(
	idx int,
	c ConstraintEntry,
	levels []ForEachLevel,
	binds []EachBinding,
	at string,
	evalAgainstInputs ConstraintEvalFunc,
	display FieldDisplay,
	errs *ErrorList,
) {
	if len(levels) == 0 {
		checkPredicateOnce(idx, c, evalAgainstInputs, binds, at, errs)
		return
	}
	lv := levels[0]
	iterable, err := evalAgainstInputs(lv.In, binds)
	if err != nil {
		errs.Addf(ErrSchema, lv.In.Span().Start,
			"constraints[%d] (predicate): @for-each: %v", idx, err)
		return
	}
	levelText := levelElementText(lv.InText, at, binds, display)
	descend := func(key any, element any, elementAt string) {
		child := append(slices.Clip(binds), EachBinding{
			Name:  lv.Name,
			Key:   key,
			Value: element,
		})
		checkPredicateLevels(idx, c, levels[1:], child, elementAt,
			evalAgainstInputs, display, errs)
	}
	switch it := iterable.(type) {
	case nil:
	case []any:
		for i, el := range it {
			descend(int64(i), el, fmt.Sprintf("%s[%d]", levelText, i))
		}
	case map[string]any:
		keys := make([]string, 0, len(it))
		for k := range it {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			descend(k, it[k], fmt.Sprintf("%s['%s']", levelText, k))
		}
	default:
		errs.Addf(ErrSchema, lv.In.Span().Start,
			"constraints[%d] (predicate): @for-each must iterate a list or map, got %s",
			idx, TypeMessage(iterable))
	}
}

// levelElementText renders the indexed source text a level's elements
// are named by. A level rooted at an enclosing binding substitutes
// that binding's own indexed text, so the failing element reads as one
// path from the input down: var.rules[1].transitions[0].
func levelElementText(
	inText, at string, binds []EachBinding, display FieldDisplay,
) string {
	for i := len(binds) - 1; i >= 0; i-- {
		name := binds[i].Name
		if rest, ok := strings.CutPrefix(inText, name+".value"); ok {
			return at + rest
		}
		if rest, ok := strings.CutPrefix(inText, name); ok {
			return at + rest
		}
	}
	return display.name(inText)
}

// checkPredicateOnce runs one when/require evaluation, with the
// iteration bindings in scope and a failure suffix naming the element
// when the predicate iterates.
func checkPredicateOnce(
	idx int,
	c ConstraintEntry,
	evalAgainstInputs ConstraintEvalFunc,
	binds []EachBinding,
	at string,
	errs *ErrorList,
) {
	suffix := ""
	if at != "" {
		suffix = " (" + at + ")"
	}
	whenVal, err := evalAgainstInputs(c.When, binds)
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
	requireVal, err := evalAgainstInputs(c.Require, binds)
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
		errs.Addf(ErrSchema, Position{},
			"constraints[%d] (predicate): %s%s", idx, msg, suffix)
	}
}

// forEachText renders an @for-each iterable for the element a failure
// names: a dotted path renders as written, anything else as @for-each.
func forEachText(e Expr) string {
	if dp, ok := e.(*DotPath); ok {
		return dotPathString(dp)
	}
	return "@for-each"
}

// readForEachLevels reads an @for-each value into its levels. The bare
// form, any non-array expression, is one level binding @each. The
// chained form is an array of level objects, each exactly one @-named
// key bound to an iterable; the names must be unique and may not take
// a reserved name. ok is false for a malformed chain, which skips the
// entry the way other malformed pieces do; compile validation reports
// the mistake.
func readForEachLevels(v Expr) ([]ForEachLevel, bool) {
	arr, ok := v.(*ArrayLit)
	if !ok {
		return []ForEachLevel{{Name: "@each", In: v, InText: forEachText(v)}}, true
	}
	if len(arr.Elements) == 0 {
		return nil, false
	}
	levels := make([]ForEachLevel, 0, len(arr.Elements))
	seen := map[string]bool{"@each": true, CoreNamespace: true}
	for _, el := range arr.Elements {
		obj, ok := el.(*ObjectLit)
		if !ok || len(obj.Fields) != 1 {
			return nil, false
		}
		f := obj.Fields[0]
		if f.Key.Kind != FieldIdent || !strings.HasPrefix(f.Key.Name, "@") {
			return nil, false
		}
		if seen[f.Key.Name] {
			return nil, false
		}
		seen[f.Key.Name] = true
		levels = append(levels, ForEachLevel{
			Name:   f.Key.Name,
			In:     f.Value,
			InText: forEachText(f.Value),
		})
	}
	return levels, true
}

func joinNames(names []string) string {
	return strings.Join(names, ", ")
}

// ConstraintSpec is the embeddable, string-only form of a constraint.
// The predicate When, Require, and iterables are kept as unobin source
// so the whole set can be generated into a factory and parsed back at
// plan time; a set constraint leaves them empty and uses Fields. A
// bare iteration uses ForEach; a chained one uses ForEachLevels, named
// levels in order. goschema produces these from a Go type, and codegen
// bakes them into the factory.
type ConstraintSpec struct {
	Kind          string
	Fields        []string
	When          string
	Require       string
	Message       string
	ForEach       string
	ForEachLevels []ForEachSpecLevel
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
		levels, ok := parseSpecLevels(s, errs)
		if !ok {
			continue
		}
		e.When = when
		e.Require = require
		e.Levels = levels
		entries = append(entries, e)
	}
	return entries, errs
}

// parseSpecLevels reads a spec's iteration into entry levels: the bare
// ForEach source as a single @each level, or each named chain level in
// order.
func parseSpecLevels(s ConstraintSpec, errs *ErrorList) ([]ForEachLevel, bool) {
	if s.ForEach != "" {
		in, ok := parseSpecExpr(s.ForEach, "@for-each", errs)
		if !ok {
			return nil, false
		}
		return []ForEachLevel{{Name: "@each", In: in, InText: s.ForEach}}, true
	}
	if len(s.ForEachLevels) == 0 {
		return nil, true
	}
	levels := make([]ForEachLevel, 0, len(s.ForEachLevels))
	for _, lv := range s.ForEachLevels {
		in, ok := parseSpecExpr(lv.In, "@for-each", errs)
		if !ok {
			return nil, false
		}
		levels = append(levels, ForEachLevel{Name: lv.Name, In: in, InText: lv.In})
	}
	return levels, true
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
