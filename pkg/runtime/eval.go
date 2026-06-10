package runtime

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"reflect"
	"slices"
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
// resolve against; nil disables library-qualified calls. Bindings holds
// comprehension-bound names, which resolve as bare values and as
// dot-path roots ahead of the reserved roots; validation keeps the
// names distinct across nesting.
type EvalContext struct {
	Vars      map[string]any
	Resources map[string]any
	Data      map[string]any
	Actions   map[string]any
	Libraries map[string]*Library
	Bindings  map[string]any

	// Each holds named iteration bindings, @each for a @for-each body
	// and declared names like @rule for a chained constraint form,
	// each a key/value record read as @name.key and @name.value.
	Each map[string]lang.EachValue

	// MissingAsNull makes path navigation yield null instead of
	// ErrEvalNotFound, or a hard error, when a key is absent or a parent
	// is itself null. The constraint checkers set it so a predicate over
	// an unset optional input, including a nested one, reduces to a
	// boolean rather than collapsing the whole expression to null or
	// failing on a null parent. Navigating into a non-null scalar is
	// still an error. It stays false everywhere else, because the planner
	// relies on ErrEvalNotFound to detect forward references to upstreams
	// that have not run yet.
	MissingAsNull bool

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
		slices.Sort(keys)
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
	return renderScalar(v), nil
}

// renderScalar returns a scalar value as text, the rendering an
// interpolation slot without a verb uses. @core.join renders elements
// through it too, so a joined element and a slot agree on what a value
// looks like as text.
func renderScalar(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return fmt.Sprintf("%v", v)
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

// evalCall evaluates a function call. A call is qualified by @core,
// the language's own namespace, or by an imported library's alias; a
// bare call has nothing to resolve against and is rejected.
func evalCall(c *lang.Call, ctx *EvalContext) (any, error) {
	if c.Library != nil {
		return evalLibraryCall(c, ctx)
	}
	name := ""
	if c.Callee != nil {
		name = c.Callee.Name
	}
	return nil, fmt.Errorf(
		"eval: function %q must be qualified with %s or an imported library, e.g. %s.%s(...)",
		name, lang.CoreNamespace, lang.CoreNamespace, name)
}

func evalLibraryCall(c *lang.Call, ctx *EvalContext) (any, error) {
	if c.Library.Name == lang.CoreNamespace {
		return evalCoreCall(c, ctx)
	}
	lib, ok := ctx.Libraries[c.Library.Name]
	if !ok {
		return nil, fmt.Errorf("eval: library %q is not imported", c.Library.Name)
	}
	fn, ok := lib.Functions[c.Func.Name]
	if !ok {
		return nil, fmt.Errorf("eval: library %s has no function %q",
			c.Library.Name, c.Func.Name)
	}
	name := c.Library.Name + "." + c.Func.Name
	args, err := evalArgs(name, c.Args, ctx)
	if err != nil {
		return nil, err
	}
	out, err := guard("calling "+name, false, func() (any, error) {
		return fn.Func(args)
	})
	blameLibrary(err, c.Library.Name)
	return out, err
}

// evalCoreCall resolves a @core call against the language's own
// function table, in scope in every context with no import.
func evalCoreCall(c *lang.Call, ctx *EvalContext) (any, error) {
	fn, ok := coreFunctions[c.Func.Name]
	if !ok {
		return nil, fmt.Errorf("eval: %s has no function %q",
			lang.CoreNamespace, c.Func.Name)
	}
	name := lang.CoreNamespace + "." + c.Func.Name
	args, err := evalArgs(name, c.Args, ctx)
	if err != nil {
		return nil, err
	}
	return guard("calling "+name, true, func() (any, error) {
		return fn.Func(args)
	})
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
	if n.Op == "??" {
		return evalCoalesce(n, ctx)
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

// evalCoalesce evaluates `??` with a short circuit: the right side
// only evaluates when the left is null.
func evalCoalesce(n *lang.Infix, ctx *EvalContext) (any, error) {
	left, err := Eval(n.Left, ctx)
	if err != nil {
		return nil, err
	}
	if left != nil {
		return left, nil
	}
	return Eval(n.Right, ctx)
}

// evalArith evaluates the four arithmetic operators. `+` also
// concatenates two strings; a string mixed with anything else is an
// error, so a value is never silently rendered into text (that is
// interpolation's job). Otherwise both operands must be numbers (int64
// or float64). When both are int64 the result stays int64 with no
// float round trip; any float operand promotes the pair and the result
// is float64. Division by zero returns an error in both modes; integer
// division truncates toward zero (Go's `/` semantics).
func evalArith(op string, a, b any) (any, error) {
	if op == "+" {
		as, aStr := a.(string)
		bs, bStr := b.(string)
		if aStr && bStr {
			return as + bs, nil
		}
	}
	if ai, ok := a.(int64); ok {
		if bi, ok := b.(int64); ok {
			return arithInt(op, ai, bi)
		}
	}
	af, aOK := numericFloat(a)
	bf, bOK := numericFloat(b)
	if !aOK || !bOK {
		if op == "+" {
			return nil, fmt.Errorf(
				"eval: +: operands must both be numbers or both be strings, got %s and %s",
				lang.TypeMessage(a), lang.TypeMessage(b))
		}
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
	if c, ok := numericCmp(a, b); ok {
		switch op {
		case "<":
			return c < 0, nil
		case "<=":
			return c <= 0, nil
		case ">":
			return c > 0, nil
		case ">=":
			return c >= 0, nil
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

// evalEq reports value equality with numeric promotion at every
// position: a pair of numbers compares by value (1 == 1.0), and the
// promotion reaches inside lists and maps, so [1] == [1.0] too. Lists
// compare element-wise and maps key-wise; anything else falls to
// reflect.DeepEqual.
func evalEq(a, b any) bool {
	if c, ok := numericCmp(a, b); ok {
		return c == 0
	}
	switch av := a.(type) {
	case []any:
		bv, ok := b.([]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for i := range av {
			if !evalEq(av[i], bv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok || len(av) != len(bv) {
			return false
		}
		for k, v := range av {
			w, ok := bv[k]
			if !ok || !evalEq(v, w) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(a, b)
}

// numericCmp orders two numeric values, returning -1, 0, or 1 with ok
// true. A pair of int64 compares in the integer domain so values past
// float64's exact range stay distinct; a mixed int64/float64 pair
// promotes to float64. ok is false when either value is not numeric.
func numericCmp(a, b any) (int, bool) {
	ai, aInt := a.(int64)
	bi, bInt := b.(int64)
	if aInt && bInt {
		return cmp.Compare(ai, bi), true
	}
	af, aOK := numericFloat(a)
	bf, bOK := numericFloat(b)
	if !aOK || !bOK {
		return 0, false
	}
	return cmp.Compare(af, bf), true
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

// evalEach resolves an iteration binding reference, @each.key or
// @rule.value.port, against the current iteration scope. Reading a
// name with no binding in scope is an error.
func evalEach(p *lang.DotPath, ctx *EvalContext) (any, error) {
	name := p.Root.Name
	binding, bound := ctx.Each[name]
	if !bound {
		if name == "@each" {
			return nil, fmt.Errorf("eval: @each is only valid inside a @for-each body")
		}
		return nil, fmt.Errorf(
			"eval: %s is not bound; declare it in a chained @for-each", name)
	}
	if len(p.Segments) == 0 {
		return nil, fmt.Errorf("eval: %s requires .key or .value", name)
	}
	first := p.Segments[0].Name
	var cur any
	switch first {
	case "key":
		cur = binding.Key
	case "value":
		cur = binding.Value
	default:
		return nil, fmt.Errorf(
			"eval: %s.%s: only %s.key and %s.value are valid", name, first, name, name)
	}
	return navigateSegments(cur, name+"."+first, p.Segments[1:], ctx)
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
	case lang.CoreNamespace:
		return nil, fmt.Errorf(
			"eval: %s names functions; call one, e.g. %s.length(...)",
			lang.CoreNamespace, lang.CoreNamespace)
	default:
		if strings.HasPrefix(p.Root.Name, "@") {
			return evalEach(p, ctx)
		}
		return nil, fmt.Errorf("eval: unknown address root %q", p.Root.Name)
	}
	return navigateSegments(root, p.Root.Name, p.Segments, ctx)
}

// navigateSegments walks a dot path's segments from cur, stepping into
// nested maps. path accumulates the source form for error messages. A
// missing key yields ErrEvalNotFound so plan can treat it as known
// after apply, unless the context set MissingAsNull, in which case a
// missing key or a null parent reads as null instead. A `?.` segment
// reads a null value as the whole path's result instead of failing.
func navigateSegments(
	cur any, path string, segs []lang.DotSegment, ctx *EvalContext,
) (any, error) {
	for i, seg := range segs {
		if cur == nil && seg.Guarded {
			return nil, nil
		}
		if cur == nil && ctx != nil && ctx.MissingAsNull {
			return nil, nil
		}
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
					return softenMissing(err, ctx)
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
				return softenMissing(err, ctx)
			}
			continue
		}
		var err error
		cur, path, err = mapStep(cur, seg.Name, path)
		if err != nil {
			return softenMissing(err, ctx)
		}
	}
	return cur, nil
}

// softenMissing converts a not-found navigation error into a null
// result when the context asked for lenient navigation, so a missing
// key or out-of-range index reads as null. Every other error, and the
// strict default, propagate unchanged.
func softenMissing(err error, ctx *EvalContext) (any, error) {
	if ctx != nil && ctx.MissingAsNull && errors.Is(err, ErrEvalNotFound) {
		return nil, nil
	}
	return nil, err
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

// ApplyBindings copies a constraint's iteration bindings onto the
// context so @each and any chained level name resolve during the
// element's evaluation.
func ApplyBindings(ctx *EvalContext, binds []lang.EachBinding) {
	if len(binds) == 0 {
		return
	}
	ctx.Each = make(map[string]lang.EachValue, len(binds))
	for _, b := range binds {
		ctx.Each[b.Name] = lang.EachValue{Key: b.Key, Value: b.Value}
	}
}
