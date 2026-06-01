package runtime

import (
	"errors"
	"fmt"
	"maps"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/cloudboss/unobin/pkg/lang"
)

// ErrEvalNotFound is returned by Eval when an address or field cannot
// be resolved in the current scope. Plan callers may treat it as
// "known after apply"; apply re-evaluates against the live scope and
// surfaces a real failure when the reference truly is invalid.
var ErrEvalNotFound = errors.New("not found")

// EvalContext supplies the values that addresses resolve against. Vars
// is the validated `inputs:` map after `config.ub` and `UB_VAR_*` env
// overrides. Resources, Data, and Actions hold the outputs of nodes
// that have already executed, indexed by their source address path.
// Libraries is the import table the scope's `<alias>.<func>(...)` calls
// resolve against; nil disables library-qualified calls. EachKey and
// EachValue carry the current iteration binding inside a `@for-each`
// body; ForEach reports whether they are valid to read. Bindings holds
// comprehension-bound names, which resolve as bare values and as
// dot-path roots ahead of the reserved roots, so an inner binding
// shadows an outer one.
type EvalContext struct {
	Vars      map[string]any
	Resources map[string]any
	Data      map[string]any
	Actions   map[string]any
	Libraries map[string]*Library
	Bindings  map[string]any
	EachKey   any
	EachValue any
	ForEach   bool

	// locals holds the scope's `locals:` declarations and folds them
	// lazily as `local.<name>` references are evaluated. Shared with
	// child contexts so a comprehension inside the scope sees the
	// same locals and reuses their memoized values.
	locals *localScope
}

// withBindings returns a shallow copy of ctx whose Bindings merges the
// parent bindings with binds. The parent is left untouched so sibling
// iterations and enclosing scopes do not see each other's bindings.
func (ctx *EvalContext) withBindings(binds map[string]any) *EvalContext {
	child := *ctx
	merged := make(map[string]any, len(ctx.Bindings)+len(binds))
	maps.Copy(merged, ctx.Bindings)
	maps.Copy(merged, binds)
	child.Bindings = merged
	return &child
}

// Eval reduces a parsed expression to a Go value. Supported are
// literals, bare identifiers (as their name string); array and object
// literals (recursive); and the `var.X[.Y...]` address form.
func Eval(e lang.Expr, ctx *EvalContext) (any, error) {
	switch v := e.(type) {
	case *lang.StringLit:
		return v.Value, nil
	case *lang.InterpolatedString:
		return evalInterpolated(v, ctx)
	case *lang.NumberLit:
		if v.IsFloat {
			return v.ParsedFloat, nil
		}
		return v.ParsedInt, nil
	case *lang.BoolLit:
		return v.Value, nil
	case *lang.NullLit:
		return nil, nil
	case *lang.Ident:
		if val, ok := ctx.Bindings[v.Name]; ok {
			return val, nil
		}
		return v.Name, nil
	case *lang.ArrayLit:
		return evalArray(v, ctx)
	case *lang.ObjectLit:
		return evalObject(v, ctx)
	case *lang.DotPath:
		return evalDotPath(v, ctx)
	case *lang.Infix:
		return evalInfix(v, ctx)
	case *lang.Prefix:
		return evalPrefix(v, ctx)
	case *lang.Call:
		return evalCall(v, ctx)
	case *lang.Conditional:
		return evalConditional(v, ctx)
	case *lang.Comprehension:
		return evalComprehension(v, ctx)
	default:
		return nil, fmt.Errorf("eval: unsupported expression %T", e)
	}
}

// evalComprehension evaluates a list or map comprehension. The source
// is reduced first; a list source iterates its elements in order and a
// map source iterates its entries by sorted key, so the output is
// deterministic.
func evalComprehension(n *lang.Comprehension, ctx *EvalContext) (any, error) {
	src, err := Eval(n.Source, ctx)
	if err != nil {
		return nil, err
	}
	binds, err := comprehensionBindings(src, n.Names)
	if err != nil {
		return nil, err
	}
	if n.Kind == lang.CompMap {
		return evalMapComp(n, binds, ctx)
	}
	return evalListComp(n, binds, ctx)
}

// comprehensionBindings turns the source value into one binding map per
// iteration. One name binds the element (list) or value (map); two
// names bind index+element (list) or key+value (map).
func comprehensionBindings(src any, names []string) ([]map[string]any, error) {
	switch s := src.(type) {
	case []any:
		out := make([]map[string]any, 0, len(s))
		for i, el := range s {
			out = append(out, bindNames(names, int64(i), el))
		}
		return out, nil
	case map[string]any:
		keys := make([]string, 0, len(s))
		for k := range s {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([]map[string]any, 0, len(s))
		for _, k := range keys {
			out = append(out, bindNames(names, k, s[k]))
		}
		return out, nil
	}
	return nil, fmt.Errorf(
		"eval: comprehension source must be a list or map, got %s", lang.TypeMessage(src))
}

// bindNames pairs the bound identifiers with the iteration's first
// (index or key) and second (element or value) positions.
func bindNames(names []string, first, second any) map[string]any {
	b := make(map[string]any, len(names))
	switch len(names) {
	case 1:
		b[names[0]] = second
	case 2:
		b[names[0]] = first
		b[names[1]] = second
	}
	return b
}

func evalListComp(n *lang.Comprehension, binds []map[string]any, ctx *EvalContext) (any, error) {
	out := make([]any, 0, len(binds))
	for _, b := range binds {
		child := ctx.withBindings(b)
		keep, err := comprehensionKeep(n.Filter, child)
		if err != nil {
			return nil, err
		}
		if !keep {
			continue
		}
		val, err := Eval(n.Value, child)
		if err != nil {
			return nil, err
		}
		out = append(out, val)
	}
	return out, nil
}

func evalMapComp(n *lang.Comprehension, binds []map[string]any, ctx *EvalContext) (any, error) {
	out := make(map[string]any, len(binds))
	for _, b := range binds {
		child := ctx.withBindings(b)
		keep, err := comprehensionKeep(n.Filter, child)
		if err != nil {
			return nil, err
		}
		if !keep {
			continue
		}
		keyVal, err := Eval(n.Key, child)
		if err != nil {
			return nil, err
		}
		key, ok := keyVal.(string)
		if !ok {
			return nil, fmt.Errorf(
				"eval: comprehension key must be a string, got %s", lang.TypeMessage(keyVal))
		}
		val, err := Eval(n.Value, child)
		if err != nil {
			return nil, err
		}
		if n.Group {
			lst, _ := out[key].([]any)
			out[key] = append(lst, val)
			continue
		}
		if _, exists := out[key]; exists {
			return nil, fmt.Errorf(
				"eval: comprehension produced duplicate key %q; use ... to group", key)
		}
		out[key] = val
	}
	return out, nil
}

// comprehensionKeep evaluates a `when` filter. A nil filter keeps every
// element; otherwise the predicate must reduce to a boolean.
func comprehensionKeep(filter lang.Expr, ctx *EvalContext) (bool, error) {
	if filter == nil {
		return true, nil
	}
	v, err := Eval(filter, ctx)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf(
			"eval: comprehension filter must be a boolean, got %s", lang.TypeMessage(v))
	}
	return b, nil
}

// evalConditional evaluates `if cond then a else b`. The condition must
// be a boolean, and only the taken branch is evaluated, so the dead
// branch never runs and never errors.
func evalConditional(n *lang.Conditional, ctx *EvalContext) (any, error) {
	cond, err := Eval(n.Cond, ctx)
	if err != nil {
		return nil, err
	}
	b, ok := cond.(bool)
	if !ok {
		return nil, fmt.Errorf(
			"eval: if: condition must be a boolean, got %s", lang.TypeMessage(cond))
	}
	if b {
		return Eval(n.Then, ctx)
	}
	return Eval(n.Else, ctx)
}

// evalInterpolated reduces an interpolated string by concatenating its
// literal runs with the rendered value of each slot. A slot must reduce
// to a scalar; a non-empty verb formats it through fmt, otherwise the
// value uses its default rendering.
func evalInterpolated(n *lang.InterpolatedString, ctx *EvalContext) (any, error) {
	var b strings.Builder
	for _, part := range n.Parts {
		if part.Expr == nil {
			b.WriteString(part.Lit)
			continue
		}
		val, err := Eval(part.Expr, ctx)
		if err != nil {
			return nil, err
		}
		s, err := interpolatedSlot(val, part.Verb)
		if err != nil {
			return nil, err
		}
		b.WriteString(s)
	}
	return b.String(), nil
}

// interpolatedSlot renders one slot value to text. A null or a composite
// value is an error; the type checker rejects most of these statically,
// but a value flowing in from a node output is guarded here too.
func interpolatedSlot(v any, verb string) (string, error) {
	switch v.(type) {
	case nil:
		return "", fmt.Errorf("eval: interpolation slot is null")
	case []any, map[string]any:
		return "", fmt.Errorf(
			"eval: interpolation slot must be a scalar, got %s", lang.TypeMessage(v))
	}
	if verb != "" {
		return fmt.Sprintf(verb, v), nil
	}
	switch x := v.(type) {
	case string:
		return x, nil
	case bool:
		return strconv.FormatBool(x), nil
	case int64:
		return strconv.FormatInt(x, 10), nil
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

func evalArray(a *lang.ArrayLit, ctx *EvalContext) ([]any, error) {
	out := make([]any, 0, len(a.Elements))
	for i, el := range a.Elements {
		val, err := Eval(el, ctx)
		if err != nil {
			return nil, fmt.Errorf("eval: array[%d]: %w", i, err)
		}
		out = append(out, val)
	}
	return out, nil
}

func evalObject(o *lang.ObjectLit, ctx *EvalContext) (map[string]any, error) {
	out := make(map[string]any, len(o.Fields))
	for _, fld := range o.Fields {
		var key string
		switch fld.Key.Kind {
		case lang.FieldIdent:
			key = fld.Key.Name
		case lang.FieldString:
			key = fld.Key.String
		}
		val, err := Eval(fld.Value, ctx)
		if err != nil {
			return nil, fmt.Errorf("eval: field %q: %w", key, err)
		}
		out[key] = val
	}
	return out, nil
}

// evalCall evaluates a function call. Every function lives in a library,
// so a call must be library-qualified (core.format(...)); a bare call has
// no library to resolve against and is rejected.
func evalCall(c *lang.Call, ctx *EvalContext) (any, error) {
	if c.Library != nil {
		return evalLibraryCall(c, ctx)
	}
	name := ""
	if c.Callee != nil {
		name = c.Callee.Name
	}
	return nil, fmt.Errorf(
		"eval: function %q must be qualified with a library, e.g. core.%s(...)", name, name)
}

func evalLibraryCall(c *lang.Call, ctx *EvalContext) (any, error) {
	lib, ok := ctx.Libraries[c.Library.Name]
	if !ok {
		return nil, fmt.Errorf("eval: library %q is not imported", c.Library.Name)
	}
	fn, ok := lib.Functions[c.Func.Name]
	if !ok {
		return nil, fmt.Errorf("eval: library %s has no function %q",
			c.Library.Name, c.Func.Name)
	}
	args, err := evalArgs(c.Library.Name+"."+c.Func.Name, c.Args, ctx)
	if err != nil {
		return nil, err
	}
	return fn.Func(args)
}

func evalArgs(name string, exprs []lang.Expr, ctx *EvalContext) ([]any, error) {
	args := make([]any, len(exprs))
	for i, a := range exprs {
		v, err := Eval(a, ctx)
		if err != nil {
			return nil, fmt.Errorf("eval: %s arg %d: %w", name, i, err)
		}
		args[i] = v
	}
	return args, nil
}

// evalInfix evaluates a binary operator expression. `&&` and `||` short
// circuit on the left operand; every other operator evaluates both sides
// before dispatching by kind.
func evalInfix(n *lang.Infix, ctx *EvalContext) (any, error) {
	if n.Op == "&&" || n.Op == "||" {
		return evalLogical(n, ctx)
	}
	left, err := Eval(n.Left, ctx)
	if err != nil {
		return nil, err
	}
	right, err := Eval(n.Right, ctx)
	if err != nil {
		return nil, err
	}
	switch n.Op {
	case "+", "-", "*", "/":
		return evalArith(n.Op, left, right)
	case "<", "<=", ">", ">=":
		return evalCmp(n.Op, left, right)
	case "==":
		return evalEq(left, right), nil
	case "!=":
		return !evalEq(left, right), nil
	}
	return nil, fmt.Errorf("eval: unknown operator %q", n.Op)
}

// evalLogical evaluates `&&` and `||` with left-to-right short circuit:
// `&&` skips its right operand when the left is false, `||` skips when
// the left is true. Either operand evaluating to a non-boolean is a
// type error.
func evalLogical(n *lang.Infix, ctx *EvalContext) (any, error) {
	left, err := Eval(n.Left, ctx)
	if err != nil {
		return nil, err
	}
	lb, ok := left.(bool)
	if !ok {
		return nil, fmt.Errorf(
			"eval: %s: left operand must be a boolean, got %s",
			n.Op, lang.TypeMessage(left))
	}
	if n.Op == "&&" && !lb {
		return false, nil
	}
	if n.Op == "||" && lb {
		return true, nil
	}
	right, err := Eval(n.Right, ctx)
	if err != nil {
		return nil, err
	}
	rb, ok := right.(bool)
	if !ok {
		return nil, fmt.Errorf(
			"eval: %s: right operand must be a boolean, got %s",
			n.Op, lang.TypeMessage(right))
	}
	return rb, nil
}

// evalArith evaluates the four arithmetic operators. Both operands must
// be numbers (int64 or float64). When both are int64 the result stays
// int64 with no float round trip; any float operand promotes the pair
// and the result is float64. Division by zero returns an error in both
// modes; integer division truncates toward zero (Go's `/` semantics).
func evalArith(op string, a, b any) (any, error) {
	if ai, ok := a.(int64); ok {
		if bi, ok := b.(int64); ok {
			return arithInt(op, ai, bi)
		}
	}
	af, aOK := numericFloat(a)
	bf, bOK := numericFloat(b)
	if !aOK || !bOK {
		return nil, fmt.Errorf(
			"eval: %s: operands must be numbers, got %s and %s",
			op, lang.TypeMessage(a), lang.TypeMessage(b))
	}
	return arithFloat(op, af, bf)
}

func arithInt(op string, a, b int64) (any, error) {
	switch op {
	case "+":
		return a + b, nil
	case "-":
		return a - b, nil
	case "*":
		return a * b, nil
	case "/":
		if b == 0 {
			return nil, fmt.Errorf("eval: division by zero")
		}
		return a / b, nil
	}
	return nil, fmt.Errorf("eval: unknown arithmetic operator %q", op)
}

func arithFloat(op string, a, b float64) (any, error) {
	switch op {
	case "+":
		return a + b, nil
	case "-":
		return a - b, nil
	case "*":
		return a * b, nil
	case "/":
		if b == 0 {
			return nil, fmt.Errorf("eval: division by zero")
		}
		return a / b, nil
	}
	return nil, fmt.Errorf("eval: unknown arithmetic operator %q", op)
}

// evalCmp evaluates the four ordering operators. Numbers compare with
// numeric promotion; strings compare lexicographically. Any other
// combination is a type error.
func evalCmp(op string, a, b any) (any, error) {
	if af, bf, ok := numericPair(a, b); ok {
		switch op {
		case "<":
			return af < bf, nil
		case "<=":
			return af <= bf, nil
		case ">":
			return af > bf, nil
		case ">=":
			return af >= bf, nil
		}
	}
	if as, aok := a.(string); aok {
		if bs, bok := b.(string); bok {
			switch op {
			case "<":
				return as < bs, nil
			case "<=":
				return as <= bs, nil
			case ">":
				return as > bs, nil
			case ">=":
				return as >= bs, nil
			}
		}
	}
	return nil, fmt.Errorf(
		"eval: %s: operands must be numbers or strings, got %s and %s",
		op, lang.TypeMessage(a), lang.TypeMessage(b))
}

// evalEq reports value equality. Numeric operands compare after
// promotion (so 1 == 1.0 is true). Everything else falls through to
// reflect.DeepEqual, which gives the expected element-wise comparison
// for lists and maps and a false answer for cross-type comparisons
// outside the numeric pair.
func evalEq(a, b any) bool {
	if af, bf, ok := numericPair(a, b); ok {
		return af == bf
	}
	return reflect.DeepEqual(a, b)
}

// numericPair coerces a and b to a common float64 form for ordering
// and equality tests. ok is false when either input is not numeric.
// Arithmetic stays in the int64 domain on its own path; this helper is
// only used where the two values are about to be compared.
func numericPair(a, b any) (af, bf float64, ok bool) {
	af, aOK := numericFloat(a)
	bf, bOK := numericFloat(b)
	if !aOK || !bOK {
		return 0, 0, false
	}
	return af, bf, true
}

func numericFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case int64:
		return float64(x), true
	case float64:
		return x, true
	}
	return 0, false
}

// evalPrefix evaluates a unary operator. `-` negates a number; `!` flips
// a boolean. Other operators or operand types yield an error.
func evalPrefix(n *lang.Prefix, ctx *EvalContext) (any, error) {
	v, err := Eval(n.Expr, ctx)
	if err != nil {
		return nil, err
	}
	switch n.Op {
	case "-":
		switch x := v.(type) {
		case int64:
			return -x, nil
		case float64:
			return -x, nil
		}
		return nil, fmt.Errorf("eval: -: operand must be a number, got %s", lang.TypeMessage(v))
	case "!":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("eval: !: operand must be a boolean, got %s", lang.TypeMessage(v))
		}
		return !b, nil
	}
	return nil, fmt.Errorf("eval: unknown prefix operator %q", n.Op)
}

// evalEach resolves an `@each.key` / `@each.value` reference against
// the current iteration scope. Reading @each outside a `@for-each`
// body is an error.
func evalEach(p *lang.DotPath, ctx *EvalContext) (any, error) {
	if !ctx.ForEach {
		return nil, fmt.Errorf("eval: @each is only valid inside a @for-each body")
	}
	if len(p.Segments) == 0 {
		return nil, fmt.Errorf("eval: @each requires .key or .value")
	}
	first := p.Segments[0].Name
	var cur any
	switch first {
	case "key":
		cur = ctx.EachKey
	case "value":
		cur = ctx.EachValue
	default:
		return nil, fmt.Errorf("eval: @each.%s: only @each.key and @each.value are valid", first)
	}
	return navigateSegments(cur, "@each."+first, p.Segments[1:], ctx)
}

func evalDotPath(p *lang.DotPath, ctx *EvalContext) (any, error) {
	if base, ok := ctx.Bindings[p.Root.Name]; ok {
		return navigateSegments(base, p.Root.Name, p.Segments, ctx)
	}
	var root any
	switch p.Root.Name {
	case "var":
		root = ctx.Vars
	case "resource":
		root = ctx.Resources
	case "data":
		root = ctx.Data
	case "action":
		root = ctx.Actions
	case "local":
		return evalLocal(p, ctx)
	case "@each":
		return evalEach(p, ctx)
	default:
		return nil, fmt.Errorf("eval: unknown address root %q", p.Root.Name)
	}
	return navigateSegments(root, p.Root.Name, p.Segments, ctx)
}

// navigateSegments walks a dot path's segments from cur, stepping into
// nested maps. path accumulates the source form for error messages. A
// missing key yields ErrEvalNotFound so plan can treat it as known
// after apply.
func navigateSegments(
	cur any, path string, segs []lang.DotSegment, ctx *EvalContext,
) (any, error) {
	for i, seg := range segs {
		if seg.Splat {
			return splatProject(cur, path, segs[i+1:], ctx)
		}
		if seg.Name == "" && seg.Index != nil {
			idx, err := Eval(seg.Index, ctx)
			if err != nil {
				return nil, err
			}
			if n, ok := idx.(int64); ok {
				cur, path, err = indexElement(cur, n, path)
				if err != nil {
					return nil, err
				}
				continue
			}
			key, ok := idx.(string)
			if !ok {
				return nil, fmt.Errorf(
					"eval: index must be a string or integer, got %s", lang.TypeMessage(idx))
			}
			cur, path, err = mapStep(cur, key, path)
			if err != nil {
				return nil, err
			}
			continue
		}
		var err error
		cur, path, err = mapStep(cur, seg.Name, path)
		if err != nil {
			return nil, err
		}
	}
	return cur, nil
}

// splatProject applies the remaining segments to each element of a list
// and collects the results, so `list[*].id` reads the id of every
// element. A non-list value cannot be projected.
func splatProject(cur any, path string, rest []lang.DotSegment, ctx *EvalContext) (any, error) {
	lst, ok := cur.([]any)
	if !ok {
		return nil, fmt.Errorf("eval: cannot splat %s at %s", lang.TypeMessage(cur), path+"[*]")
	}
	out := make([]any, 0, len(lst))
	for n, el := range lst {
		elem, err := navigateSegments(el, fmt.Sprintf("%s[%d]", path, n), rest, ctx)
		if err != nil {
			return nil, err
		}
		out = append(out, elem)
	}
	return out, nil
}

// mapStep steps into a map by key, extending path for diagnostics. A
// non-map value or a missing key fails; a missing key reports
// ErrEvalNotFound so plan can treat it as known after apply.
func mapStep(cur any, key, path string) (any, string, error) {
	path = path + "." + key
	m, ok := cur.(map[string]any)
	if !ok {
		return nil, path, fmt.Errorf(
			"eval: cannot navigate into %s at %s", lang.TypeMessage(cur), path)
	}
	next, exists := m[key]
	if !exists {
		return nil, path, fmt.Errorf("eval: %s: %w", path, ErrEvalNotFound)
	}
	return next, path, nil
}

// indexElement steps into a list by position, extending path for
// diagnostics. A non-list value fails; an out-of-range index, negative
// included, reports ErrEvalNotFound so plan can treat a list that is
// still filling in as known after apply.
func indexElement(cur any, i int64, path string) (any, string, error) {
	path = fmt.Sprintf("%s[%d]", path, i)
	lst, ok := cur.([]any)
	if !ok {
		return nil, path, fmt.Errorf(
			"eval: cannot index into %s at %s", lang.TypeMessage(cur), path)
	}
	if i < 0 || i >= int64(len(lst)) {
		return nil, path, fmt.Errorf("eval: %s: %w", path, ErrEvalNotFound)
	}
	return lst[i], path, nil
}
