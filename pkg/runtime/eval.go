package runtime

import (
	"errors"
	"fmt"
	"reflect"

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
// Modules is the import table the scope's `<alias>.<func>(...)` calls
// resolve against; nil disables module-qualified calls. EachKey and
// EachValue carry the current iteration binding inside a `@for-each`
// body; ForEach reports whether they are valid to read.
type EvalContext struct {
	Vars      map[string]any
	Resources map[string]any
	Data      map[string]any
	Actions   map[string]any
	Modules   map[string]*Module
	EachKey   any
	EachValue any
	ForEach   bool
}

// Eval reduces a parsed expression to a Go value. Supported are
// literals, bare identifiers (as their name string); array and object
// literals (recursive); and the `var.X[.Y...]` address form.
func Eval(e lang.Expr, ctx *EvalContext) (any, error) {
	switch v := e.(type) {
	case *lang.StringLit:
		return v.Value, nil
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
	default:
		return nil, fmt.Errorf("eval: unsupported expression %T", e)
	}
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

// evalCall evaluates a function call. Bare identifiers (`format(...)`)
// look up the built-in registry; module-qualified calls
// (`alias.func(...)`) resolve against the scope's Modules table.
func evalCall(c *lang.Call, ctx *EvalContext) (any, error) {
	if c.Module != nil {
		return evalModuleCall(c, ctx)
	}
	if c.Callee == nil {
		return nil, fmt.Errorf("eval: call has no callee")
	}
	fn, ok := builtins[c.Callee.Name]
	if !ok {
		return nil, fmt.Errorf("eval: unknown function %q", c.Callee.Name)
	}
	args, err := evalArgs(c.Callee.Name, c.Args, ctx)
	if err != nil {
		return nil, err
	}
	return fn(args)
}

func evalModuleCall(c *lang.Call, ctx *EvalContext) (any, error) {
	mod, ok := ctx.Modules[c.Module.Name]
	if !ok {
		return nil, fmt.Errorf("eval: module %q is not imported", c.Module.Name)
	}
	fn, ok := mod.Functions[c.Func.Name]
	if !ok {
		return nil, fmt.Errorf("eval: module %s has no function %q",
			c.Module.Name, c.Func.Name)
	}
	args, err := evalArgs(c.Module.Name+"."+c.Func.Name, c.Args, ctx)
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
	path := "@each." + first
	for _, seg := range p.Segments[1:] {
		var step string
		switch {
		case seg.Name != "":
			step = seg.Name
		case seg.Index != nil:
			idx, err := Eval(seg.Index, ctx)
			if err != nil {
				return nil, err
			}
			s, ok := idx.(string)
			if !ok {
				return nil, fmt.Errorf("eval: index must be a string, got %s", lang.TypeMessage(idx))
			}
			step = s
		}
		path = path + "." + step
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf(
				"eval: cannot navigate into %s at %s", lang.TypeMessage(cur), path)
		}
		next, exists := m[step]
		if !exists {
			return nil, fmt.Errorf("eval: %s: %w", path, ErrEvalNotFound)
		}
		cur = next
	}
	return cur, nil
}

func evalDotPath(p *lang.DotPath, ctx *EvalContext) (any, error) {
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
	case "@each":
		return evalEach(p, ctx)
	default:
		return nil, fmt.Errorf("eval: unknown address root %q", p.Root.Name)
	}
	cur := root
	path := p.Root.Name
	for _, seg := range p.Segments {
		var step string
		switch {
		case seg.Name != "":
			step = seg.Name
		case seg.Index != nil:
			idx, err := Eval(seg.Index, ctx)
			if err != nil {
				return nil, err
			}
			s, ok := idx.(string)
			if !ok {
				return nil, fmt.Errorf("eval: index must be a string, got %s", lang.TypeMessage(idx))
			}
			step = s
		}
		path = path + "." + step
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, fmt.Errorf(
				"eval: cannot navigate into %s at %s", lang.TypeMessage(cur), path)
		}
		next, exists := m[step]
		if !exists {
			return nil, fmt.Errorf("eval: %s: %w", path, ErrEvalNotFound)
		}
		cur = next
	}
	return cur, nil
}
